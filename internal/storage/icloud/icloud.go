// Package icloud stores encrypted objects as immutable revision records in a
// dedicated, locally downloaded iCloud shared folder. It cannot lock other Macs.
package icloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/d-jiao/codex-sync/internal/storage"
)

const marker = "codex-sync-format"
const format = "codex-sync-icloud-v1\n"
const maxRecordSize = storage.MaxDownloadSize*4/3 + 16384

var ErrReadOnly = errors.New("iCloud remote deletion is disabled: absence and locks are not reliable across Macs")

type Store struct {
	folder string
	mu     sync.Mutex
	scanMu sync.Mutex
	cache  map[string]cachedRevision
}
type record struct {
	Key     string    `json:"key"`
	Parent  string    `json:"parent,omitempty"`
	Created time.Time `json:"created"`
	Data    []byte    `json:"data"`
}
type cachedRevision struct {
	revision revision
	info     os.FileInfo
}

type revision struct {
	record
	id   string
	size int64
}

func init() { storage.NewICloud = New }

// Prepare explicitly initializes an empty, existing dedicated folder. It never
// creates a missing root (which could mask a detached or misspelled iCloud path).
func Prepare(folder string) error {
	s := &Store{folder: folder}
	r, err := s.open()
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Stat(marker); err == nil {
		return s.checkMarker(r)
	}
	entries, err := readDir(r)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			return fmt.Errorf("shared folder must be empty or already initialized; found %s", e.Name())
		}
	}
	f, err := r.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(format)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func New(cfg *storage.StorageConfig) (storage.Storage, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	s := &Store{folder: filepath.Clean(cfg.SharedFolder)}
	r, err := s.open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err = s.checkMarker(r); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) open() (*os.Root, error) {
	if !filepath.IsAbs(s.folder) || filepath.Clean(s.folder) == string(filepath.Separator) {
		return nil, fmt.Errorf("shared folder must be an absolute dedicated folder")
	}
	// Resolve normal macOS ancestor aliases (e.g. /var -> /private/var), but
	// never accept a symlink for the selected directory itself.
	fi, err := os.Lstat(s.folder)
	if err != nil {
		return nil, fmt.Errorf("shared folder unavailable; download/accept it in Finder: %w", err)
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("shared folder must be a real directory, not a symlink")
	}
	return os.OpenRoot(s.folder)
}
func (s *Store) checkMarker(r *os.Root) error {
	b, err := readRegular(r, marker, 1024)
	if err != nil {
		return fmt.Errorf("iCloud folder not initialized or not downloaded; run init: %w", err)
	}
	if string(b) != format {
		return fmt.Errorf("unsupported iCloud folder format")
	}
	return nil
}
func readDir(r *os.Root) ([]os.DirEntry, error) {
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return f.ReadDir(-1)
}
func readRegular(r *os.Root, name string, limit int64) ([]byte, error) {
	st, e := r.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular downloaded file: %s", name)
	}
	if st.Size() > limit {
		return nil, fmt.Errorf("oversized iCloud record: %s", name)
	}
	f, e := r.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("oversized iCloud record: %s", name)
	}
	return b, nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validID(id string) bool {
	b, e := hex.DecodeString(id)
	return e == nil && len(b) == 32 && strings.ToLower(id) == id
}
func validKey(key string) bool {
	return key != "" && len(key) < 4096 && path.Clean(key) == key && !strings.HasPrefix(key, "/") && key != ".." && !strings.HasPrefix(key, "../") && !strings.ContainsAny(key, "\\\x00:")
}

// scan validates the whole locally visible replica before exposing any objects.
func (s *Store) scan(ctx context.Context) (map[string]revision, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if s.cache == nil {
		s.cache = make(map[string]cachedRevision)
	}
	r, e := s.open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	if e = s.checkMarker(r); e != nil {
		return nil, e
	}
	entries, e := readDir(r)
	if e != nil {
		return nil, e
	}
	records := map[string]revision{}
	parents := map[string]bool{}
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		name := entry.Name()
		if name == marker || name == ".DS_Store" || strings.HasPrefix(name, ".tmp-") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !validID(id) || name != id+".json" {
			return nil, fmt.Errorf("unexpected iCloud item %q; finish Finder downloads and resolve cloud conflict copies", name)
		}
		st, err := r.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("not a regular downloaded file: %s", name)
		}
		// Cache only metadata for verified immutable files. Enumerate every time
		// so missing parents and newly delivered forks remain visible. Downloads
		// always re-read and verify ciphertext, regardless of this cache.
		if cached, ok := s.cache[id]; ok && os.SameFile(cached.info, st) && cached.info.Size() == st.Size() && cached.info.ModTime().Equal(st.ModTime()) {
			records[id] = cached.revision
			continue
		}
		b, err := readRegular(r, name, maxRecordSize)
		if err != nil {
			return nil, err
		}
		if digest(b) != id {
			return nil, fmt.Errorf("incomplete or corrupt iCloud revision %s; wait for Finder synchronization", id)
		}
		var rec record
		if err = json.Unmarshal(b, &rec); err != nil {
			return nil, err
		}
		if !validKey(rec.Key) || len(rec.Data) > storage.MaxDownloadSize || rec.Created.IsZero() || (rec.Parent != "" && !validID(rec.Parent)) {
			return nil, fmt.Errorf("invalid iCloud revision %s", id)
		}
		size := int64(len(rec.Data))
		rec.Data = nil // keep only metadata; history must not accumulate ciphertext in RAM
		records[id] = revision{record: rec, id: id, size: size}
		s.cache[id] = cachedRevision{revision: records[id], info: st}
	}
	for _, v := range records {
		if v.Parent != "" {
			p, ok := records[v.Parent]
			if !ok || p.Key != v.Key {
				return nil, fmt.Errorf("incomplete iCloud history for %s; missing parent %s; wait for Finder synchronization", v.Key, v.Parent)
			}
			parents[v.Parent] = true
		}
	}
	heads := map[string]revision{}
	for id, v := range records {
		if parents[id] {
			continue
		}
		if old, ok := heads[v.Key]; ok {
			return nil, fmt.Errorf("iCloud concurrent revisions for %s (%s, %s); both retained; see docs/icloud.md recovery", v.Key, old.id, id)
		}
		heads[v.Key] = v
	}
	return heads, nil
}
func info(v revision) *storage.ObjectInfo {
	return &storage.ObjectInfo{Key: v.Key, Size: v.size, LastModified: v.Created, Version: v.id, ETag: v.id}
}
func (s *Store) List(ctx context.Context, prefix string) ([]storage.ObjectInfo, error) {
	h, e := s.scan(ctx)
	if e != nil {
		return nil, e
	}
	out := []storage.ObjectInfo{}
	for k, v := range h {
		if strings.HasPrefix(k, prefix) {
			out = append(out, *info(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
func (s *Store) Head(ctx context.Context, key string) (*storage.ObjectInfo, error) {
	h, e := s.scan(ctx)
	if e != nil {
		return nil, e
	}
	v, ok := h[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info(v), nil
}
func (s *Store) Download(ctx context.Context, key string) ([]byte, error) {
	h, e := s.scan(ctx)
	if e != nil {
		return nil, e
	}
	v, ok := h[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return s.DownloadRevision(ctx, key, v.id)
}
func (s *Store) BucketExists(ctx context.Context) (bool, error) {
	_, e := s.scan(ctx)
	return e == nil, e
}
func (s *Store) Delete(context.Context, string) error        { return ErrReadOnly }
func (s *Store) DeleteBatch(context.Context, []string) error { return ErrReadOnly }

func (s *Store) Upload(ctx context.Context, key string, data []byte) error {
	// Used for metadata; session writes use UploadRevision and the sync state.
	s.mu.Lock()
	defer s.mu.Unlock()
	h, e := s.scan(ctx)
	if e != nil {
		return e
	}
	_, e = s.publish(ctx, key, data, h[key].id)
	return e
}
func (s *Store) UploadRevision(ctx context.Context, key string, data []byte, expected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, e := s.scan(ctx)
	if e != nil {
		return "", e
	}
	if h[key].id != expected {
		return "", fmt.Errorf("%w: %s; pull before pushing", storage.ErrPreconditionFailed, key)
	}
	return s.publish(ctx, key, data, expected)
}
func (s *Store) publish(ctx context.Context, key string, data []byte, parent string) (string, error) {
	if e := ctx.Err(); e != nil {
		return "", e
	}
	if !validKey(key) || len(data) > storage.MaxDownloadSize {
		return "", fmt.Errorf("invalid key or oversized object")
	}
	b, e := json.Marshal(record{Key: key, Parent: parent, Created: time.Now().UTC(), Data: data})
	if e != nil {
		return "", e
	}
	id := digest(b)
	r, e := s.open()
	if e != nil {
		return "", e
	}
	defer r.Close()
	if e = s.checkMarker(r); e != nil {
		return "", e
	}
	// Write outside the published namespace, fsync, then rename atomically on
	// this filesystem. A content-addressed name never replaces another revision.
	name := ".tmp-" + id
	f, e := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", e
	}
	defer r.Remove(name)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return "", e
	}
	if ce != nil {
		return "", ce
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	if e = renameRecord(r, name, id+".json"); e != nil {
		return "", e
	}
	return id, nil
}

func (s *Store) DownloadRevision(ctx context.Context, key, version string) ([]byte, error) {
	actual, data, err := s.ExportRevision(ctx, version)
	if err != nil {
		return nil, err
	}
	if actual != key {
		return nil, fmt.Errorf("revision key mismatch")
	}
	return data, nil
}

// ExportRevision retrieves one retained record even if the replica has forks.
// It never changes the shared folder or selects a winning branch.
func (s *Store) ExportRevision(ctx context.Context, version string) (string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if !validID(version) {
		return "", nil, fmt.Errorf("invalid revision")
	}
	r, err := s.open()
	if err != nil {
		return "", nil, err
	}
	defer r.Close()
	if err = s.checkMarker(r); err != nil {
		return "", nil, err
	}
	b, err := readRegular(r, version+".json", maxRecordSize)
	if err != nil {
		return "", nil, err
	}
	if digest(b) != version {
		return "", nil, fmt.Errorf("corrupt iCloud revision %s", version)
	}
	var rec record
	if err = json.Unmarshal(b, &rec); err != nil {
		return "", nil, err
	}
	if !validKey(rec.Key) || len(rec.Data) > storage.MaxDownloadSize {
		return "", nil, fmt.Errorf("invalid iCloud revision")
	}
	return rec.Key, rec.Data, nil
}
