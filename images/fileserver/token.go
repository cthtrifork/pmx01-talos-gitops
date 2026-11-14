package main

import (
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errUnauthorized = errors.New("missing or invalid token")

// The upload token lives in a Secret that the rotator (rotate.go) writes and the pod mounts as files:
//
//	current     the token CI must send
//	previous    the token before the last rotation
//	rotated-at  RFC 3339 time of the last rotation
//
// Files are read on every request, so a rotation reaches the pod when the kubelet refreshes the mounted
// Secret (about a minute) without a restart. The previous token is accepted for a grace period so that a job
// that started before a rotation can still finish its upload.
type TokenAuth struct {
	dir   string
	grace time.Duration
	now   func() time.Time
}

func NewTokenAuth(dir string, grace time.Duration) *TokenAuth {
	return &TokenAuth{dir: dir, grace: grace, now: time.Now}
}

func (a *TokenAuth) read(name string) string {
	b, err := os.ReadFile(filepath.Join(a.dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (a *TokenAuth) rotatedAt() time.Time {
	t, _ := time.Parse(time.RFC3339, a.read("rotated-at"))
	return t
}

// Check accepts "Bearer <token>" if the token is the current one, or the previous one within the grace period.
func (a *TokenAuth) Check(header string) error {
	tok, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || tok == "" {
		return errUnauthorized
	}
	if cur := a.read("current"); cur != "" && eq(tok, cur) {
		return nil
	}
	if prev := a.read("previous"); prev != "" && eq(tok, prev) {
		if r := a.rotatedAt(); !r.IsZero() && a.now().Sub(r) < a.grace {
			return nil
		}
	}
	return errUnauthorized
}

func eq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// TokenInfo is what the admin page shows.
type TokenInfo struct {
	Current       string     `json:"current"`
	RotatedAt     *time.Time `json:"rotatedAt,omitempty"`
	PreviousUntil *time.Time `json:"previousValidUntil,omitempty"`
}

func (a *TokenAuth) Info() TokenInfo {
	info := TokenInfo{Current: a.read("current")}
	if r := a.rotatedAt(); !r.IsZero() {
		info.RotatedAt = &r
		if a.read("previous") != "" {
			u := r.Add(a.grace)
			if u.After(a.now()) {
				info.PreviousUntil = &u
			}
		}
	}
	return info
}
