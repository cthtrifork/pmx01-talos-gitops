package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	errUnauthorized = errors.New("missing or invalid token")
	errForbidden    = errors.New("repository not allowed")
)

// Verifier checks GitHub Actions OIDC tokens: signature (keys fetched from the issuer and cached by go-oidc),
// issuer, audience and expiry, then that the "repository" claim is on the allowlist. There is no static secret.
type Verifier struct {
	issuer, audience string
	repos            map[string]bool

	mu sync.Mutex
	v  *oidc.IDTokenVerifier
}

func NewVerifier(issuer, audience string, repos []string) *Verifier {
	m := map[string]bool{}
	for _, r := range repos {
		if r = strings.TrimSpace(r); r != "" {
			m[r] = true
		}
	}
	return &Verifier{issuer: issuer, audience: audience, repos: m}
}

// verifier discovers the issuer on first use, so the pod starts and stays healthy while GitHub is unreachable;
// uploads fail with 401 until discovery works. The context outlives the request because go-oidc keeps it for
// background key fetches.
func (a *Verifier) verifier() (*oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.v != nil {
		return a.v, nil
	}
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 15 * time.Second})
	p, err := oidc.NewProvider(ctx, a.issuer)
	if err != nil {
		return nil, err
	}
	a.v = p.Verifier(&oidc.Config{ClientID: a.audience})
	return a.v, nil
}

// Check returns the repository of a valid token from the Authorization header.
func (a *Verifier) Check(ctx context.Context, header string) (string, error) {
	tok, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || tok == "" {
		return "", errUnauthorized
	}
	v, err := a.verifier()
	if err != nil {
		return "", errUnauthorized
	}
	idt, err := v.Verify(ctx, tok)
	if err != nil {
		return "", errUnauthorized
	}
	var c struct {
		Repository string `json:"repository"`
	}
	if err := idt.Claims(&c); err != nil || c.Repository == "" {
		return "", errUnauthorized
	}
	if !a.repos[c.Repository] {
		return "", errForbidden
	}
	return c.Repository, nil
}
