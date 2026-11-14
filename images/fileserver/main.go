// fileserver: a small file server in three modes, one container each in the same pod (see README.md).
//
//	--mode=public  GET /<name> from the public directory, read-only mount
//	--mode=admin   admin page and JSON API, reached through the Omni service exposer
//	--mode=upload  PUT /v1/files/<name> for CI, authenticated with a bearer token rotated by --mode=rotate
//	--mode=rotate  CronJob: creates and rotates the upload token Secret in the pod and runner namespaces
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		slog.Error("invalid number", "env", key, "value", v)
		os.Exit(2)
	}
	return n
}

// tokenAuth reads the mounted token Secret. The directory may not exist yet (before the first rotation): uploads
// are refused until it does, the pod itself stays up.
func tokenAuth() *TokenAuth {
	grace, err := time.ParseDuration(env("PREVIOUS_TOKEN_GRACE", "24h"))
	fatal(err)
	return NewTokenAuth(env("TOKEN_DIR", "/var/run/fileserver-token"), grace)
}

func main() {
	mode := flag.String("mode", "", "public, admin, upload or rotate")
	flag.Parse()

	dataDir := env("DATA_DIR", "/data")
	maxBytes := envInt("MAX_FILE_BYTES", 8<<30)
	minFree := envInt("MIN_FREE_BYTES", 256<<20)
	healthAddr := env("HEALTH_ADDR", ":9102")

	var handler http.Handler
	var addr string
	switch *mode {
	case "public":
		// The public container mounts only public/ (read-only), at PUBLIC_DIR.
		root, err := os.OpenRoot(env("PUBLIC_DIR", dataDir+"/public"))
		fatal(err)
		handler, addr = publicHandler(root), env("LISTEN_ADDR", ":8080")
	case "admin":
		s, err := NewStore(dataDir, uint64(minFree))
		fatal(err)
		handler, addr = adminHandler(s, tokenAuth(), maxBytes), env("LISTEN_ADDR", ":8081")
	case "upload":
		s, err := NewStore(dataDir, uint64(minFree))
		fatal(err)
		handler, addr = uploadHandler(s, tokenAuth(), maxBytes), env("LISTEN_ADDR", ":8082")
	case "rotate":
		fatal(runRotate())
		return
	default:
		fatal(fmt.Errorf("--mode must be public, admin, upload or rotate"))
	}

	// No overall read/write timeout: images are several GiB. Header and idle timeouts still bound slow clients.
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	health := &http.Server{Addr: healthAddr, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		http.NotFound(w, r)
	})}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	errc := make(chan error, 2)
	go func() { errc <- srv.ListenAndServe() }()
	go func() { errc <- health.ListenAndServe() }()
	slog.Info("listening", "mode", *mode, "addr", addr)

	select {
	case err := <-errc:
		fatal(err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	_ = health.Shutdown(shutdown)
}

func fatal(err error) {
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
