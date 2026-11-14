package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// The rotator (--mode=rotate) runs as a CronJob. It keeps one Secret in several namespaces in step: the pod's own
// namespace (read by the upload and admin containers) and the runner namespace (read by the ARC runners). It
// speaks to the Kubernetes API directly with the standard library, so the image needs no kubectl and no client library.
//
// It is idempotent: if the first target is younger than maxAge it only repairs the other targets (a missing or
// edited copy), otherwise it rotates. Running it daily with a 30 day maxAge rotates every 30 days.

type secretRef struct{ Namespace, Name string }

type secretObj struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   secretMeta        `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type secretMeta struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
}

var errSecretNotFound = errors.New("secret not found")

type kube struct {
	base, token string
	hc          *http.Client
}

const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

func newKube() (*kube, error) {
	base := env("KUBE_API", "")
	if base == "" {
		h, p := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		if h == "" || p == "" {
			return nil, errors.New("not in a cluster: set KUBE_API")
		}
		base = "https://" + h + ":" + p
	}
	tok, err := os.ReadFile(env("KUBE_TOKEN_FILE", saDir+"/token"))
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{}
	if strings.HasPrefix(base, "https://") {
		ca, err := os.ReadFile(env("KUBE_CA_FILE", saDir+"/ca.crt"))
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &kube{base: base, token: strings.TrimSpace(string(tok)), hc: &http.Client{Transport: tr, Timeout: 30 * time.Second}}, nil
}

func (k *kube) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.base+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+k.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

func (k *kube) get(ctx context.Context, r secretRef) (*secretObj, error) {
	b, code, err := k.do(ctx, "GET", "/api/v1/namespaces/"+r.Namespace+"/secrets/"+r.Name, nil)
	if err != nil {
		return nil, err
	}
	switch code {
	case http.StatusOK:
		var s secretObj
		return &s, json.Unmarshal(b, &s)
	case http.StatusNotFound:
		return nil, errSecretNotFound
	}
	return nil, fmt.Errorf("get %s/%s: HTTP %d", r.Namespace, r.Name, code)
}

// put creates the Secret, or replaces it when it exists (rv is its resourceVersion).
func (k *kube) put(ctx context.Context, r secretRef, data map[string][]byte, existing *secretObj) error {
	s := secretObj{APIVersion: "v1", Kind: "Secret", Type: "Opaque", Data: data,
		Metadata: secretMeta{Name: r.Name, Namespace: r.Namespace, Labels: map[string]string{"app.kubernetes.io/managed-by": "fileserver-rotator"}}}
	method, path := "POST", "/api/v1/namespaces/"+r.Namespace+"/secrets"
	if existing != nil {
		s.Metadata.ResourceVersion = existing.Metadata.ResourceVersion
		method, path = "PUT", path+"/"+r.Name
	}
	_, code, err := k.do(ctx, method, path, s)
	if err != nil {
		return err
	}
	if code != http.StatusOK && code != http.StatusCreated {
		return fmt.Errorf("%s %s/%s: HTTP %d", method, r.Namespace, r.Name, code)
	}
	return nil
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "fs_" + hex.EncodeToString(b[:]), nil
}

func sameData(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// rotate decides from the first target whether to rotate, then writes the result to every target.
func rotate(ctx context.Context, k *kube, targets []secretRef, maxAge time.Duration, now time.Time) (rotated bool, err error) {
	if len(targets) == 0 {
		return false, errors.New("no targets")
	}
	var data map[string][]byte
	cur, err := k.get(ctx, targets[0])
	switch {
	case err == nil:
		data = cur.Data
	case errors.Is(err, errSecretNotFound):
	default:
		return false, err
	}
	at, _ := time.Parse(time.RFC3339, string(data["rotated-at"]))
	if len(data["current"]) == 0 || at.IsZero() || now.Sub(at) >= maxAge {
		tok, err := newToken()
		if err != nil {
			return false, err
		}
		data = map[string][]byte{"current": []byte(tok), "rotated-at": []byte(now.UTC().Format(time.RFC3339))}
		if old := cur; old != nil && len(old.Data["current"]) > 0 {
			data["previous"] = old.Data["current"]
		}
		rotated = true
	}
	for _, t := range targets {
		existing, err := k.get(ctx, t)
		if err != nil && !errors.Is(err, errSecretNotFound) {
			return rotated, err
		}
		if err == nil && sameData(existing.Data, data) {
			continue
		}
		if errors.Is(err, errSecretNotFound) {
			existing = nil
		}
		if err := k.put(ctx, t, data, existing); err != nil {
			return rotated, err
		}
		slog.Info("secret written", "namespace", t.Namespace, "name", t.Name, "rotated", rotated)
	}
	return rotated, nil
}

func parseTargets(s string) ([]secretRef, error) {
	var out []secretRef
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		ns, name, ok := strings.Cut(p, "/")
		if !ok || ns == "" || name == "" {
			return nil, fmt.Errorf("target %q must be namespace/name", p)
		}
		out = append(out, secretRef{ns, name})
	}
	if len(out) == 0 {
		return nil, errors.New("ROTATE_TARGETS is empty")
	}
	return out, nil
}

func runRotate() error {
	targets, err := parseTargets(os.Getenv("ROTATE_TARGETS"))
	if err != nil {
		return err
	}
	maxAge, err := time.ParseDuration(env("ROTATE_AFTER", "720h"))
	if err != nil {
		return err
	}
	k, err := newKube()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rotated, err := rotate(ctx, k, targets, maxAge, time.Now())
	if err != nil {
		return err
	}
	slog.Info("done", "rotated", rotated)
	return nil
}
