package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d-jiao/codex-sync/internal/config"
	"github.com/d-jiao/codex-sync/internal/crypto"
	"github.com/d-jiao/codex-sync/internal/storage"
	"github.com/d-jiao/codex-sync/internal/storage/icloud"
)

func TestICloudTwoAccountsAndStaleWriter(t *testing.T) {
	ctx := context.Background()
	folder := t.TempDir()
	if err := icloud.Prepare(folder); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := crypto.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	peer := func() *Syncer {
		root := t.TempDir()
		base := filepath.Join(root, "codex")
		if err := os.Mkdir(base, 0700); err != nil {
			t.Fatal(err)
		}
		cfg := &config.Config{Storage: &storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: folder}, EncryptionKey: key, BaseDirOverride: base, StateDirOverride: filepath.Join(root, "state"), Scope: config.ScopeFull, SyncPaths: []string{"."}}
		s, err := NewSyncer(cfg, true)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := peer(), peer()
	for i, s := range []*Syncer{a, b} {
		identity := []string{"account-A", "account-B"}[i]
		writeFile(t, s.claudeDir, "auth.json", identity)
		writeFile(t, s.claudeDir, "installation_id", identity)
		writeFile(t, s.claudeDir, "config.toml", identity)
		writeFile(t, s.claudeDir, "state_5.sqlite", identity)
	}
	run := func(s *Syncer, push bool) {
		t.Helper()
		var r *SyncResult
		var err error
		if push {
			r, err = s.Push(ctx)
		} else {
			r, err = s.Pull(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Errors) > 0 {
			t.Fatal(r.Errors)
		}
	}
	rel := "sessions/2026/10/10/rollout.jsonl"
	writeFile(t, a.claudeDir, rel, "first\n")
	writeFile(t, a.claudeDir, "session_index.jsonl", "{\"id\":\"a\",\"thread_name\":\"A\"}\n")
	run(a, true)
	run(b, false)
	got, err := os.ReadFile(filepath.Join(b.claudeDir, rel))
	if err != nil || string(got) != "first\n" {
		t.Fatalf("%s %v", got, err)
	}
	for i, s := range []*Syncer{a, b} {
		for _, name := range []string{"auth.json", "installation_id", "config.toml", "state_5.sqlite"} {
			got, err = os.ReadFile(filepath.Join(s.claudeDir, name))
			if err != nil || string(got) != []string{"account-A", "account-B"}[i] {
				t.Fatalf("identity changed: %s %s %v", name, got, err)
			}
		}
	}
	objects, err := a.storage.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objects {
		if strings.Contains(o.Key, "auth") || strings.Contains(o.Key, "config.toml") || strings.Contains(o.Key, "sqlite") {
			t.Fatalf("leaked %s", o.Key)
		}
	}
	writeFile(t, a.claudeDir, rel, "new on A\n")
	run(a, true)
	writeFile(t, b.claudeDir, rel, "new on B\n")
	r, err := b.Push(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Errors) == 0 {
		t.Fatal("stale push accepted")
	}
	run(b, false)
	matches, err := filepath.Glob(filepath.Join(b.claudeDir, rel+".conflict.*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("conflict not retained: %v %v", matches, err)
	}
	b.SetAllowRemoteDeletes(true)
	if _, err = b.Push(ctx); err == nil {
		t.Fatal("forced delete accepted")
	}
	// Temporarily losing a known latest revision must not roll A back to its
	// still-visible parent, even though that partial history has a single head.
	version := a.state.GetFile(rel).RemoteVersion
	if err = os.Remove(filepath.Join(folder, version+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Pull(ctx); err == nil {
		t.Fatal("accepted rollback to an older replica")
	}
	got, err = os.ReadFile(filepath.Join(a.claudeDir, rel))
	if err != nil || string(got) != "new on A\n" {
		t.Fatalf("rolled back: %s %v", got, err)
	}

}

func TestICloudPartialListingNeverRemovesLocal(t *testing.T) {
	env := setupTestEnv(t)
	env.syncer.cfg.Storage = &storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: t.TempDir()}
	writeFile(t, env.claudeDir, "sessions/keep.jsonl", "keep")
	if r, e := env.syncer.Push(context.Background()); e != nil || len(r.Errors) > 0 {
		t.Fatalf("%+v %v", r, e)
	}
	// The mock represents an internally consistent but incomplete cloud replica.
	if e := env.store.Delete(context.Background(), env.syncer.remoteKey("sessions/keep.jsonl")); e != nil {
		t.Fatal(e)
	}
	seedRemote(t, env, "sessions/other.jsonl", "other")
	env.syncer.SetNoDelete(false)
	preview, e := env.syncer.PreviewPull(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.WouldRemove) != 0 {
		t.Fatalf("unsafe preview: %+v", preview)
	}
	if r, e := env.syncer.Pull(context.Background()); e != nil || len(r.Errors) > 0 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = os.Stat(filepath.Join(env.claudeDir, "sessions/keep.jsonl")); e != nil {
		t.Fatal("removed local data", e)
	}
}
