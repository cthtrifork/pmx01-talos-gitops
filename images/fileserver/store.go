package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Visibility is the directory a file lives in. There is no other metadata.
type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

func (v Visibility) valid() bool { return v == Public || v == Private }

func (v Visibility) other() Visibility {
	if v == Public {
		return Private
	}
	return Public
}

var (
	ErrBadName  = errors.New("invalid file name")
	ErrNotFound = errors.New("not found")
	ErrClash    = errors.New("name exists with the other visibility")
	ErrTooLarge = errors.New("file too large")
	ErrNoSpace  = errors.New("not enough free space")
	ErrBadSum   = errors.New("X-Content-SHA256 must be 64 hex characters")
	ErrChecksum = errors.New("body does not match X-Content-SHA256")
)

var sumRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// A name starts with a letter, digit or underscore (no dotfiles, no "..") and has no path separator.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

func validName(n string) bool { return nameRe.MatchString(n) }

// Entry is one file as shown in the admin list.
type Entry struct {
	Name       string     `json:"name"`
	Visibility Visibility `json:"visibility"`
	Size       int64      `json:"size"`
	Modified   time.Time  `json:"modified"`
}

// Store keeps files in <dir>/public, <dir>/private and <dir>/.tmp. Every access goes through os.Root, so a
// name can never leave the directory, whatever the validation above lets through.
type Store struct {
	dir     string
	root    *os.Root
	minFree uint64
	mu      sync.Mutex // serialises the clash check and the rename, not the copy
}

func NewStore(dir string, minFree uint64) (*Store, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{"public", "private", ".tmp"} {
		if err := root.Mkdir(d, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	s := &Store{dir: dir, root: root, minFree: minFree}
	s.cleanTmp()
	return s, nil
}

// cleanTmp removes the leftovers of uploads that were cut off by a restart.
func (s *Store) cleanTmp() {
	_ = s.root.RemoveAll(".tmp")
	_ = s.root.Mkdir(".tmp", 0o755)
}

func (s *Store) freeBytes() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func (s *Store) exists(v Visibility, name string) bool {
	fi, err := s.root.Stat(string(v) + "/" + name)
	return err == nil && fi.Mode().IsRegular()
}

// Put streams r into a temporary file and renames it into place, so a half upload is never visible and an
// existing file is replaced only by a complete one. sizeHint is the Content-Length or 0 if unknown.
// wantSum is an optional hex SHA-256 of the whole body; when set, a body that hashes differently is refused.
func (s *Store) Put(name string, vis Visibility, r io.Reader, max, sizeHint int64, wantSum string) (int64, error) {
	if !validName(name) || !vis.valid() {
		return 0, ErrBadName
	}
	wantSum = strings.ToLower(strings.TrimSpace(wantSum))
	if wantSum != "" && !sumRe.MatchString(wantSum) {
		return 0, ErrBadSum
	}
	if sizeHint > max {
		return 0, ErrTooLarge
	}
	if s.exists(vis.other(), name) {
		return 0, ErrClash
	}
	free, err := s.freeBytes()
	if err != nil {
		return 0, err
	}
	if free < s.minFree+uint64(max0(sizeHint)) {
		return 0, ErrNoSpace
	}

	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	tmp := ".tmp/" + hex.EncodeToString(b[:])
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	cleanup := func() { _ = f.Close(); _ = s.root.Remove(tmp) }

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	if err != nil {
		cleanup()
		if errors.Is(err, syscall.ENOSPC) {
			return 0, ErrNoSpace
		}
		return 0, err
	}
	if n > max {
		cleanup()
		return 0, ErrTooLarge
	}
	if wantSum != "" && hex.EncodeToString(h.Sum(nil)) != wantSum {
		cleanup()
		return 0, ErrChecksum
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return 0, err
	}
	if err := f.Close(); err != nil {
		_ = s.root.Remove(tmp)
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists(vis.other(), name) {
		_ = s.root.Remove(tmp)
		return 0, ErrClash
	}
	if err := s.root.Rename(tmp, string(vis)+"/"+name); err != nil {
		_ = s.root.Remove(tmp)
		return 0, err
	}
	return n, nil
}

func max0(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

// Open returns a regular file for download.
func (s *Store) Open(vis Visibility, name string) (*os.File, fs.FileInfo, error) {
	if !validName(name) || !vis.valid() {
		return nil, nil, ErrNotFound
	}
	return openRegular(s.root, string(vis)+"/"+name)
}

func openRegular(root *os.Root, path string) (*os.File, fs.FileInfo, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, ErrNotFound
	}
	return f, fi, nil
}

func (s *Store) List() ([]Entry, error) {
	var out []Entry
	for _, v := range []Visibility{Public, Private} {
		d, err := s.root.Open(string(v))
		if err != nil {
			return nil, err
		}
		ents, err := d.ReadDir(-1)
		_ = d.Close()
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			fi, err := e.Info()
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			out = append(out, Entry{Name: e.Name(), Visibility: v, Size: fi.Size(), Modified: fi.ModTime().UTC()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) Delete(name string) error {
	if !validName(name) {
		return ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, v := range []Visibility{Public, Private} {
		if s.exists(v, name) {
			if err := s.root.Remove(string(v) + "/" + name); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

// Move changes a file's visibility by renaming it into the other directory.
func (s *Store) Move(name string, to Visibility) error {
	if !validName(name) || !to.valid() {
		return ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exists(to, name) {
		return nil
	}
	if !s.exists(to.other(), name) {
		return ErrNotFound
	}
	if err := s.root.Rename(string(to.other())+"/"+name, string(to)+"/"+name); err != nil {
		return fmt.Errorf("move: %w", err)
	}
	return nil
}
