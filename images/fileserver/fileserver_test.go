package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	_, err := s.Put(name, v, strings.NewReader(body), 1<<20, 0, "")
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
	_, err := s.Put("keep", Public, strings.NewReader("0123456789"), 5, 0, "")
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
	h := adminHandler(s, NewTokenAuth(t.TempDir(), time.Hour), 1<<20)

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

func writeToken(t *testing.T, dir, cur, prev string, at time.Time) {
	t.Helper()
	for k, v := range map[string]string{"current": cur, "previous": prev, "rotated-at": at.UTC().Format(time.RFC3339)} {
		if v == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTokenAuth(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewTokenAuth(dir, 24*time.Hour)
	if err := a.Check("Bearer anything"); err == nil {
		t.Fatal("no Secret yet: every token must be refused")
	}
	writeToken(t, dir, "new-token", "old-token", now.Add(-time.Hour))
	cases := map[string]bool{
		"Bearer new-token": true, "Bearer old-token": true, // previous is valid inside the grace period
		"Bearer wrong": false, "Bearer ": false, "new-token": false, "Basic new-token": false, "": false,
	}
	for h, ok := range cases {
		if err := a.Check(h); (err == nil) != ok {
			t.Errorf("Check(%q): ok=%v, want %v", h, err == nil, ok)
		}
	}
	writeToken(t, dir, "new-token", "old-token", now.Add(-25*time.Hour))
	if err := a.Check("Bearer old-token"); err == nil {
		t.Error("previous token must expire after the grace period")
	}
	if err := a.Check("Bearer new-token"); err != nil {
		t.Error("current token must stay valid")
	}
}

func TestUploadWithToken(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "tok", "", time.Now())
	s := newStore(t)
	h := uploadHandler(s, NewTokenAuth(dir, time.Hour), 1<<20)
	send := func(auth, path string) int {
		req := httptest.NewRequest("PUT", path, strings.NewReader("data"))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := send("", "/v1/files/a"); c != 401 {
		t.Errorf("no token: %d", c)
	}
	if c := send("Bearer nope", "/v1/files/a"); c != 401 {
		t.Errorf("bad token: %d", c)
	}
	if c := send("Bearer tok", "/v1/files/a?visibility=private"); c != 201 {
		t.Errorf("good token: %d", c)
	}
	if c := send("Bearer tok", "/v1/files/a?visibility=private"); c != 201 { // CI always overwrites
		t.Errorf("overwrite: %d", c)
	}
	if c := send("Bearer tok", "/v1/files/a?visibility=public"); c != 409 {
		t.Errorf("clash: %d", c)
	}
	if c := send("Bearer tok", "/v1/files/x?visibility=nope"); c != 400 {
		t.Errorf("bad visibility: %d", c)
	}
	if c := send("Bearer tok", "/v1/files/.hidden"); c != 400 {
		t.Errorf("bad name: %d", c)
	}
}

func TestAdminShowsToken(t *testing.T) {
	dir := t.TempDir()
	writeToken(t, dir, "tok", "", time.Now())
	h := adminHandler(newStore(t), NewTokenAuth(dir, time.Hour), 1<<20)
	req := httptest.NewRequest("GET", "/api/token", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("without header: %d", rec.Code)
	}
	req.Header.Set("X-Requested-With", "fileserver")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var info TokenInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if rec.Code != 200 || info.Current != "tok" || info.RotatedAt == nil {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	// the public handler must know nothing about tokens
	root, _ := os.OpenRoot(t.TempDir())
	rec = httptest.NewRecorder()
	publicHandler(root).ServeHTTP(rec, httptest.NewRequest("GET", "/api/token", nil))
	if rec.Code == 200 {
		t.Fatal("public handler served /api/token")
	}
}

// fakeKube is just enough of the Secrets API for the rotator.
type fakeKube struct {
	srv     *httptest.Server
	secrets map[string]*secretObj
	writes  int
}

func newFakeKube(t *testing.T) *fakeKube {
	f := &fakeKube{secrets: map[string]*secretObj{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sa-token" {
			w.WriteHeader(401)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/"), "/") // ns, "secrets"[, name]
		ns := parts[0]
		switch r.Method {
		case "GET":
			s, ok := f.secrets[ns+"/"+parts[2]]
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(s)
		case "POST", "PUT":
			var s secretObj
			_ = json.NewDecoder(r.Body).Decode(&s)
			key := ns + "/" + s.Metadata.Name
			_, exists := f.secrets[key]
			if (r.Method == "POST") == exists {
				w.WriteHeader(409)
				return
			}
			s.Metadata.ResourceVersion = "1"
			f.secrets[key] = &s
			f.writes++
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(s)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestRotate(t *testing.T) {
	f := newFakeKube(t)
	k := &kube{base: f.srv.URL, token: "sa-token", hc: f.srv.Client()}
	targets := []secretRef{{"fileserver", "tok"}, {"arc", "tok"}}
	month := 30 * 24 * time.Hour
	now := time.Now()

	// first run creates both copies with the same token
	if r, err := rotate(t.Context(), k, targets, month, now); err != nil || !r {
		t.Fatalf("first run: rotated=%v err=%v", r, err)
	}
	a, b := f.secrets["fileserver/tok"], f.secrets["arc/tok"]
	if a == nil || b == nil || !sameData(a.Data, b.Data) || !strings.HasPrefix(string(a.Data["current"]), "fs_") || len(a.Data["previous"]) != 0 {
		t.Fatalf("bad first state: %+v %+v", a, b)
	}
	first := string(a.Data["current"])

	// a run inside the period changes nothing
	w := f.writes
	if r, err := rotate(t.Context(), k, targets, month, now.Add(24*time.Hour)); err != nil || r || f.writes != w {
		t.Fatalf("early run: rotated=%v writes=%d err=%v", r, f.writes-w, err)
	}

	// a deleted copy is repaired without rotating
	delete(f.secrets, "arc/tok")
	if r, err := rotate(t.Context(), k, targets, month, now.Add(48*time.Hour)); err != nil || r || f.secrets["arc/tok"] == nil {
		t.Fatalf("repair: rotated=%v err=%v", r, err)
	}

	// after 30 days it rotates, keeps the old token as previous, and updates both copies
	if r, err := rotate(t.Context(), k, targets, month, now.Add(31*24*time.Hour)); err != nil || !r {
		t.Fatalf("rotation: rotated=%v err=%v", r, err)
	}
	a, b = f.secrets["fileserver/tok"], f.secrets["arc/tok"]
	if string(a.Data["current"]) == first || string(a.Data["previous"]) != first || !sameData(a.Data, b.Data) {
		t.Fatalf("bad rotated state: %+v", a.Data)
	}
}

func TestParseTargets(t *testing.T) {
	if got, err := parseTargets("a/b, c/d"); err != nil || len(got) != 2 || got[1] != (secretRef{"c", "d"}) {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range []string{"", "ab", "/x", "x/"} {
		if _, err := parseTargets(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestPutChecksum(t *testing.T) {
	s := newStore(t)
	sum := sha256.Sum256([]byte("payload"))
	good := hex.EncodeToString(sum[:])
	put := func(body, want string) error {
		_, err := s.Put("img", Private, strings.NewReader(body), 1<<20, 0, want)
		return err
	}
	if err := put("payload", strings.ToUpper(good)); err != nil { // case-insensitive
		t.Fatalf("matching sum: %v", err)
	}
	if err := put("truncated", good); !errors.Is(err, ErrChecksum) {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
	if err := put("payload", "xyz"); !errors.Is(err, ErrBadSum) {
		t.Fatalf("want ErrBadSum, got %v", err)
	}
	f, _, _ := s.Open(Private, "img")
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "payload" {
		t.Fatalf("a refused upload replaced the good file: %q", b)
	}
	if ents, _ := os.ReadDir(filepath.Join(s.dir, ".tmp")); len(ents) != 0 {
		t.Fatalf("temp files left: %d", len(ents))
	}
}
