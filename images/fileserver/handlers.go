package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
)

//go:embed web/index.html
var webFS embed.FS

// downloadHeaders makes every file a download, never a page: a hosted HTML or SVG file must not run on this origin.
func downloadHeaders(w http.ResponseWriter, name string) {
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
}

func serveFile(w http.ResponseWriter, r *http.Request, f *os.File, name string, fi os.FileInfo) {
	defer f.Close()
	downloadHeaders(w, name)
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrBadName):
		code = http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, ErrClash):
		code = http.StatusConflict
	case errors.Is(err, ErrTooLarge):
		code = http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrNoSpace):
		code = http.StatusInsufficientStorage
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		code = http.StatusRequestEntityTooLarge
	}
	if code == http.StatusInternalServerError {
		slog.Error("request failed", "err", err)
		http.Error(w, "internal error", code)
		return
	}
	http.Error(w, err.Error(), code)
}

// publicHandler serves GET/HEAD /<name> from the public directory only. No listing, no other methods.
func publicHandler(root *os.Root) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !validName(name) {
			http.NotFound(w, r)
			return
		}
		f, fi, err := openRegular(root, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=300")
		serveFile(w, r, f, name, fi)
	})
	return mux
}

// uploadHandler is the CI endpoint: PUT /v1/files/<name>?visibility=public|private with a GitHub OIDC bearer token.
// An existing file with the same name in the same visibility is replaced.
func uploadHandler(s *Store, a *Verifier, max int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/files/{name}", func(w http.ResponseWriter, r *http.Request) {
		repo, err := a.Check(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			if errors.Is(err, errForbidden) {
				slog.Warn("upload refused", "reason", "repository not allowed")
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		vis := Visibility(r.URL.Query().Get("visibility"))
		if vis == "" {
			vis = Private
		}
		name := r.PathValue("name")
		if !vis.valid() {
			http.Error(w, "visibility must be public or private", http.StatusBadRequest)
			return
		}
		body := http.MaxBytesReader(w, r.Body, max)
		n, err := s.Put(name, vis, body, max, r.ContentLength)
		if err != nil {
			slog.Warn("upload failed", "repository", repo, "name", name, "visibility", vis, "err", err)
			httpError(w, err)
			return
		}
		slog.Info("uploaded", "repository", repo, "name", name, "visibility", vis, "bytes", n)
		writeJSON(w, http.StatusCreated, Entry{Name: name, Visibility: vis, Size: n})
	})
	return mux
}

// adminHandler serves the admin page and its JSON API. It has no login of its own: only the Omni service exposer
// reaches it. State-changing calls need a custom header so a page on another origin cannot trigger them.
func adminHandler(s *Store, max int64) http.Handler {
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Requested-With") != "fileserver" {
				http.Error(w, "missing X-Requested-With header", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := webFS.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.List()
		if err != nil {
			httpError(w, err)
			return
		}
		if list == nil {
			list = []Entry{}
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("POST /api/files", guard(func(w http.ResponseWriter, r *http.Request) {
		vis := Visibility(r.URL.Query().Get("visibility"))
		if vis == "" {
			vis = Private
		}
		if !vis.valid() {
			http.Error(w, "visibility must be public or private", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max+(1<<20)) // the file plus multipart framing
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "expected multipart/form-data", http.StatusBadRequest)
			return
		}
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				httpError(w, err)
				return
			}
			if part.FormName() != "file" || part.FileName() == "" {
				continue
			}
			name := part.FileName()
			n, err := s.Put(name, vis, part, max, 0)
			if err != nil {
				slog.Warn("admin upload failed", "name", name, "err", err)
				httpError(w, err)
				return
			}
			slog.Info("admin uploaded", "name", name, "visibility", vis, "bytes", n)
			writeJSON(w, http.StatusCreated, Entry{Name: name, Visibility: vis, Size: n})
			return
		}
		http.Error(w, "no file part", http.StatusBadRequest)
	}))
	mux.HandleFunc("DELETE /api/files/{name}", guard(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.PathValue("name")); err != nil {
			httpError(w, err)
			return
		}
		slog.Info("admin deleted", "name", r.PathValue("name"))
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/files/{name}/visibility", guard(func(w http.ResponseWriter, r *http.Request) {
		to := Visibility(r.URL.Query().Get("to"))
		if err := s.Move(r.PathValue("name"), to); err != nil {
			httpError(w, err)
			return
		}
		slog.Info("admin moved", "name", r.PathValue("name"), "to", to)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /download/{vis}/{name}", func(w http.ResponseWriter, r *http.Request) {
		f, fi, err := s.Open(Visibility(r.PathValue("vis")), r.PathValue("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveFile(w, r, f, r.PathValue("name"), fi)
	})
	return mux
}
