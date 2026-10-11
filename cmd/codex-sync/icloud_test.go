package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"github.com/d-jiao/codex-sync/internal/config"
	"github.com/d-jiao/codex-sync/internal/crypto"
	"github.com/d-jiao/codex-sync/internal/storage"
	"github.com/d-jiao/codex-sync/internal/storage/icloud"
	"os"
	"path/filepath"
	"testing"
)

func TestICloudInitFlagsAndWizard(t *testing.T) {
	cmd := initCmd()
	if cmd.Flags().Lookup("shared-folder") == nil {
		t.Fatal("missing shared-folder flag")
	}
	folder := t.TempDir()
	cfg, err := runICloudWizard(folder)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != storage.ProviderICloud || cfg.SharedFolder != folder {
		t.Fatalf("%+v", cfg)
	}
	store, err := storage.New(cfg)
	if err != nil || store == nil {
		t.Fatal(err)
	}
	if _, err = runICloudWizard("relative/path"); err == nil {
		t.Fatal("accepted relative path")
	}
}

func TestRecoveryFileNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovered.jsonl")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("session data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeRecoveryFile(path, compressed.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writeRecoveryFile(path, []byte("overwrite")); err == nil {
		t.Fatal("overwrote recovery")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "session data" {
		t.Fatalf("%s %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe permissions: %v %v", info, err)
	}
}

func TestICloudRemoteResetKeepsLocalFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, _ := seedConfigDir(t, home)
	if err := config.Save(&config.Config{Storage: &storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: t.TempDir()}}); err != nil {
		t.Fatal(err)
	}
	cmd := resetCmd()
	cmd.SetArgs([]string{"--remote", "--force"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("accepted remote reset")
	}
	for _, name := range []string{"config.yaml", "age-key.txt", "state.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestICloudWrongPassphrasePreservesKey(t *testing.T) {
	folder := t.TempDir()
	if err := icloud.Prepare(folder); err != nil {
		t.Fatal(err)
	}
	store, err := icloud.New(&storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: folder})
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	ctx := context.Background()
	if err = deriveICloudKey(ctx, store, key, "correct-test-passphrase"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := enc.Encrypt([]byte("session"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Upload(ctx, "sessions/a.age", data); err != nil {
		t.Fatal(err)
	}
	if err = deriveICloudKey(ctx, store, key, "incorrect-test-passphrase"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	after, err := os.ReadFile(key)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("key lost", err)
	}
}
