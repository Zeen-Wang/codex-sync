package config

import (
	"github.com/d-jiao/codex-sync/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestICloudScopeAndIdentity(t *testing.T) {
	cfg := &Config{Storage: &storage.StorageConfig{Provider: storage.ProviderICloud}, Scope: ScopeFull, SyncPaths: []string{"."}}
	if got := cfg.GetEffectiveSyncPaths(); len(got) != len(SessionSyncPaths) {
		t.Fatalf("scope widened: %v", got)
	}
	for _, p := range []string{"auth.json", "installation_id", "config.toml", "rules/a", "sessions/state.sqlite", "session_index.jsonl.conflict.1"} {
		if !cfg.IsExcluded(p) {
			t.Errorf("allowed %s", p)
		}
	}
	for _, p := range []string{"sessions/a.jsonl", "archived_sessions/a.jsonl", "attachments/a.png", "history.jsonl"} {
		if cfg.IsExcluded(p) {
			t.Errorf("excluded %s", p)
		}
	}
}
func TestICloudPathSeparation(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "shared")
	if e := os.Mkdir(folder, 0700); e != nil {
		t.Fatal(e)
	}
	cfg := &Config{Storage: &storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: folder}, BaseDirOverride: filepath.Join(root, "local"), StateDirOverride: filepath.Join(root, "state"), EncryptionKey: filepath.Join(root, "keys/key")}
	if e := cfg.ValidateICloudPaths(); e != nil {
		t.Fatal(e)
	}
	for _, base := range []string{root, folder, filepath.Join(folder, "codex")} {
		cfg.BaseDirOverride = base
		if e := cfg.ValidateICloudPaths(); e == nil {
			t.Errorf("accepted overlap %s", base)
		}
	}
	alias := filepath.Join(root, "alias")
	if e := os.Symlink(folder, alias); e != nil {
		t.Fatal(e)
	}
	cfg.BaseDirOverride = filepath.Join(alias, "codex")
	if e := cfg.ValidateICloudPaths(); e == nil {
		t.Fatal("accepted aliased overlap")
	}
}
