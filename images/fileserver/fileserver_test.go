package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crypto/rand"
	"crypto/rsa"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, s *Store, name string, v Visibility, body string) error {
	t.Helper()
	_, err := s.Put(name, v, strings.NewReader(body), 1<<20, 0)
	return err
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"a", "image-1.0.qcow2", "_x", "A_b-c.d"} {
		if !validName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", ".", "..", ".hidden", "a/b", "../x", "a b", "a\x00b", strings.Repeat("a", 129)} {
		if validName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestPutOverwriteAndClash(t *testing.T) {
	s := newStore(t)
	if err := put(t, s, "a.bin", Private, "one"); err != nil {
		t.Fatal(err)
	}
	if err := put(t, s, "a.bin", Private, "two"); err != nil { // same visibility: always overwrite
		t.Fatal(err)
	}
	f, _, err := s.Open(Private, "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	if err := put(t, s, "a.bin", Public, "x"); !errors.Is(err, ErrClash) { // other visibility: refused
		t.Fatalf("want ErrClash, got %v", err)
	}
	if _, _, err := s.Open(Public, "a.bin"); err == nil {
		t.Fatal("clash must not leave a public copy")
	}
}

func TestPutTooLargeLeavesNothing(t *testing.T) {
	s := newStore(t)
	_ = put(t, s, "keep", Public, "good")
	_, err := s.Put("keep", Public, strings.NewReader("0123456789"), 5, 0)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	f, _, _ := s.Open(Public, "keep")
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "good" {
		t.Fatalf("existing file damaged: %q", b)
	}
	ents, _ := os.ReadDir(filepath.Join(s.dir, ".tmp"))
	if len(ents) != 0 {
		t.Fatalf("temp files left: %d", len(ents))
	}
}

func TestPutNoSpace(t *testing.T) {
	s := newStore(t)
	s.minFree = ^uint64(0) >> 1
	if err := put(t, s, "x", Private, "x"); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("want ErrNoSpace, got %v", err)
	}
}

func TestMoveListDelete(t *testing.T) {
	s := newStore(t)
	_ = put(t, s, "p", Public, "1")
	_ = put(t, s, "q", Private, "22")
	if err := s.Move("q", Public); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 2 || list[1].Name != "q" || list[1].Visibility != Public || list[1].Size != 2 {
		t.Fatalf("unexpected list %+v", list)
	}
	if err := s.Move("missing", Public); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := s.Delete("p"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("p"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestPublicNeverServesPrivate(t *testing.T) {
	s := newStore(t)
	_ = put(t, s, "open.txt", Public, "hello")
	_ = put(t, s, "secret.txt", Private, "nope")
	root, err := os.OpenRoot(filepath.Join(s.dir, "public"))
	if err != nil {
		t.Fatal(err)
	}
	h := publicHandler(root)

	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	if rec := get("GET", "/open.txt"); rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("open: %d %q", rec.Code, rec.Body.String())
	} else if rec.Header().Get("Content-Disposition") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("download headers missing")
	}
	for _, p := range []string{"/secret.txt", "/", "/private/secret.txt", "/../private/secret.txt", "/.tmp", "/%2e%2e/private/secret.txt"} {
		if rec := get("GET", p); rec.Code == 200 {
			t.Errorf("GET %s returned 200", p)
		}
	}
	if rec := get("POST", "/open.txt"); rec.Code == 200 {
		t.Error("POST must not be served")
	}
	if rec := get("DELETE", "/open.txt"); rec.Code == 200 {
		t.Error("DELETE must not be served")
	}
}

func TestAdminAPI(t *testing.T) {
	s := newStore(t)
	h := adminHandler(s, 1<<20)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "doc.txt")
	_, _ = fw.Write([]byte("content"))
	_ = mw.Close()

	do := func(method, url string, body io.Reader, ct string, hdr bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, body)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if hdr {
			req.Header.Set("X-Requested-With", "fileserver")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("POST", "/api/files?visibility=public", bytes.NewReader(buf.Bytes()), mw.FormDataContentType(), false); rec.Code != 403 {
		t.Fatalf("no CSRF header: want 403, got %d", rec.Code)
	}
	if rec := do("POST", "/api/files?visibility=public", bytes.NewReader(buf.Bytes()), mw.FormDataContentType(), true); rec.Code != 201 {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	rec := do("GET", "/api/files", nil, "", false)
	var list []Entry
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != "doc.txt" || list[0].Visibility != Public {
		t.Fatalf("list: %s", rec.Body)
	}
	if rec := do("POST", "/api/files/doc.txt/visibility?to=private", nil, "", true); rec.Code != 204 {
		t.Fatalf("move: %d", rec.Code)
	}
	if rec := do("GET", "/download/private/doc.txt", nil, "", false); rec.Code != 200 || rec.Body.String() != "content" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body)
	}
	if rec := do("DELETE", "/api/files/doc.txt", nil, "", true); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
}

// fakeIssuer is a minimal OIDC provider: discovery document, JWKS and a token signer.
type fakeIssuer struct {
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "jwks_uri": f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) token(t *testing.T, key *rsa.PrivateKey, iss, aud, repo string, exp time.Time) string {
	t.Helper()
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jwt.Signed(sig).Claims(jwt.Claims{Issuer: iss, Audience: jwt.Audience{aud}, Expiry: jwt.NewNumericDate(exp), IssuedAt: jwt.NewNumericDate(time.Now())}).
		Claims(map[string]any{"repository": repo}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestUploadAuth(t *testing.T) {
	iss := newFakeIssuer(t)
	s := newStore(t)
	v := NewVerifier(iss.srv.URL, "fileserver", []string{"cthtrifork/homeserver-bootc"})
	h := uploadHandler(s, v, 1<<20)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	future := time.Now().Add(time.Hour)

	send := func(token, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	good := iss.token(t, iss.key, iss.srv.URL, "fileserver", "cthtrifork/homeserver-bootc", future)

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"no token", "", 401},
		{"garbage", "abc.def.ghi", 401},
		{"wrong signature", iss.token(t, other, iss.srv.URL, "fileserver", "cthtrifork/homeserver-bootc", future), 401},
		{"wrong audience", iss.token(t, iss.key, iss.srv.URL, "other", "cthtrifork/homeserver-bootc", future), 401},
		{"wrong issuer", iss.token(t, iss.key, "https://evil.example", "fileserver", "cthtrifork/homeserver-bootc", future), 401},
		{"expired", iss.token(t, iss.key, iss.srv.URL, "fileserver", "cthtrifork/homeserver-bootc", time.Now().Add(-time.Hour)), 401},
		{"other repository", iss.token(t, iss.key, iss.srv.URL, "fileserver", "cthtrifork/other", future), 403},
		{"allowed repository", good, 201},
	}
	for _, c := range cases {
		if rec := send(c.token, "/v1/files/img.qcow2?visibility=private", "data"); rec.Code != c.want {
			t.Errorf("%s: want %d, got %d (%s)", c.name, c.want, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
	if _, _, err := s.Open(Private, "img.qcow2"); err != nil {
		t.Fatal("authorised upload not stored")
	}
	if rec := send(good, "/v1/files/img.qcow2?visibility=private", "newer"); rec.Code != 201 { // CI always overwrites
		t.Fatalf("overwrite: %d", rec.Code)
	}
	if rec := send(good, "/v1/files/img.qcow2?visibility=public", "x"); rec.Code != 409 {
		t.Fatalf("clash: want 409, got %d", rec.Code)
	}
	if rec := send(good, "/v1/files/x?visibility=nope", "x"); rec.Code != 400 {
		t.Fatalf("bad visibility: want 400, got %d", rec.Code)
	}
	if rec := send(good, "/v1/files/.hidden", "x"); rec.Code != 400 {
		t.Fatalf("bad name: want 400, got %d", rec.Code)
	}
}
