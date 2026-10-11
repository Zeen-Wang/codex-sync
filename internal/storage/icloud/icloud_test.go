package icloud

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d-jiao/codex-sync/internal/storage"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	if e := Prepare(dir); e != nil {
		t.Fatal(e)
	}
	s, e := New(&storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: dir})
	if e != nil {
		t.Fatal(e)
	}
	return s.(*Store)
}
func publish(t *testing.T, s *Store, key, data, parent string) string {
	t.Helper()
	id, e := s.UploadRevision(context.Background(), key, []byte(data), parent)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func replicate(t *testing.T, src, dst *Store) {
	t.Helper()
	entries, e := os.ReadDir(src.folder)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(src.folder, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dst.folder, entry.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestRoundTripHistoryAndCAS(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := "sessions/space #?.jsonl.age"
	a := publish(t, s, key, "encrypted-first", "")
	b := publish(t, s, key, "encrypted-second", a)
	got, e := s.Download(ctx, key)
	if e != nil || string(got) != "encrypted-second" {
		t.Fatalf("%s %v", got, e)
	}
	old, e := s.DownloadRevision(ctx, key, a)
	if e != nil || string(old) != "encrypted-first" {
		t.Fatalf("%s %v", old, e)
	}
	h, e := s.Head(ctx, key)
	if e != nil || h.Version != b || h.Size != 16 {
		t.Fatalf("%+v %v", h, e)
	}
	list, e := s.List(ctx, "sessions/")
	if e != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, e)
	}
	if _, e = s.UploadRevision(ctx, key, []byte("stale"), a); !errors.Is(e, storage.ErrPreconditionFailed) {
		t.Fatal(e)
	}
	if e = s.Delete(ctx, key); !errors.Is(e, ErrReadOnly) {
		t.Fatal(e)
	}
	if e = s.DeleteBatch(ctx, []string{key}); !errors.Is(e, ErrReadOnly) {
		t.Fatal(e)
	}
	if _, e = s.Head(ctx, "missing"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if _, e = s.Download(ctx, "missing"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	if ok, e := s.BucketExists(ctx); !ok || e != nil {
		t.Fatal(e)
	}
	if e = s.Upload(ctx, "_metadata/manifest.json.age", []byte("manifest")); e != nil {
		t.Fatal(e)
	}
}
func TestOfflineForkRetainsBothAndFailsClosed(t *testing.T) {
	a, b := newStore(t), newStore(t)
	ctx := context.Background()
	key := "sessions/a.age"
	base := publish(t, a, key, "base", "")
	replicate(t, a, b)
	left := publish(t, a, key, "left", base)
	right := publish(t, b, key, "right", base)
	replicate(t, a, b)
	replicate(t, b, a)
	for _, s := range []*Store{a, b} {
		if _, e := s.List(ctx, ""); e == nil || !strings.Contains(e.Error(), "concurrent revisions") {
			t.Fatal(e)
		}
		for id, want := range map[string]string{left: "left", right: "right"} {
			got, e := s.DownloadRevision(ctx, key, id)
			if e != nil || string(got) != want {
				t.Fatalf("lost branch %s: %v", id, e)
			}
		}
		if e := s.Upload(ctx, key, []byte("overwrite")); e == nil {
			t.Fatal("overwrote fork")
		}
	}
}
func TestMissingParentAndCorruption(t *testing.T) {
	a, b := newStore(t), newStore(t)
	key := "sessions/a.age"
	base := publish(t, a, key, "base", "")
	next := publish(t, a, key, "next", base)
	data, e := os.ReadFile(filepath.Join(a.folder, next+".json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(b.folder, next+".json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = b.List(context.Background(), ""); e == nil || !strings.Contains(e.Error(), "missing parent") {
		t.Fatal(e)
	}
	replicate(t, a, b)
	if _, e = b.List(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(b.folder, next+".json"), []byte("partial"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = b.List(context.Background(), ""); e == nil {
		t.Fatal("accepted corruption")
	}
}
func TestRejectUnsafePathsAndCloudArtifacts(t *testing.T) {
	for _, name := range []string{".revision.json.icloud", "conflicted copy.json", "directory", "link"} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			p := filepath.Join(s.folder, name)
			var e error
			switch name {
			case "directory":
				e = os.Mkdir(p, 0700)
			case "link":
				e = os.Symlink(t.TempDir(), p)
			default:
				e = os.WriteFile(p, []byte("x"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.List(context.Background(), ""); e == nil {
				t.Fatal("accepted unexpected item")
			}
		})
	}
	s := newStore(t)
	for _, key := range []string{"../escape", "/absolute", "a/../b", "a\\b", "", "a:drive"} {
		if e := s.Upload(context.Background(), key, []byte("x")); e == nil {
			t.Fatalf("accepted %q", key)
		}
	}
	if e := os.WriteFile(filepath.Join(s.folder, ".tmp-interrupted"), []byte("partial"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.List(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Upload(ctx, "a", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestPrepareAndAvailability(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if e := Prepare(missing); e == nil {
		t.Fatal("created absent root")
	}
	nonempty := t.TempDir()
	if e := os.WriteFile(filepath.Join(nonempty, "auth.json"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Prepare(nonempty); e == nil {
		t.Fatal("accepted unrelated folder")
	}
	s := newStore(t)
	if e := Prepare(s.folder); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(t.TempDir(), "link")
	if e := os.Symlink(s.folder, alias); e != nil {
		t.Fatal(e)
	}
	if e := Prepare(alias); e == nil {
		t.Fatal("accepted symlink")
	}
	if e := os.Remove(filepath.Join(s.folder, marker)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.List(context.Background(), ""); e == nil {
		t.Fatal("accepted missing marker")
	}
}
func TestLocalLock(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	release, e := s.Lock(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	other := &Store{folder: s.folder}
	if _, e = other.Lock(ctx); e == nil {
		t.Fatal("double lock")
	}
	release()
	release2, e := other.Lock(ctx)
	if e != nil {
		t.Fatal(e)
	}
	release2()
}
