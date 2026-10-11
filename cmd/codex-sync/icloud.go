package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/d-jiao/codex-sync/internal/crypto"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/d-jiao/codex-sync/internal/config"
	"github.com/d-jiao/codex-sync/internal/storage"
	"github.com/d-jiao/codex-sync/internal/storage/icloud"
)

func runICloudWizard(folder string) (*storage.StorageConfig, error) {
	printWarning("iCloud is eventually consistent. Use one Mac at a time; wait for Finder sync, then pull before editing. No distributed lock is available.")
	printInfo("Explicitly share a dedicated folder with the other Apple Account with edit permission; Family Sharing alone is insufficient.")
	printInfo("Keep the folder downloaded. Only sessions sync; logins, config, databases and keys stay local. Remote deletion is disabled.")
	if folder == "" {
		if err := survey.AskOne(&survey.Input{Message: "Local path of the existing shared folder:"}, &folder, survey.WithValidator(survey.Required)); err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(folder, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		folder = filepath.Join(home, folder[2:])
	}
	cfg := &storage.StorageConfig{Provider: storage.ProviderICloud, SharedFolder: filepath.Clean(folder)}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	local := &config.Config{Storage: cfg, EncryptionKey: config.AgeKeyFilePath()}
	if err := local.ValidateICloudPaths(); err != nil {
		return nil, err
	}
	if err := icloud.Prepare(cfg.SharedFolder); err != nil {
		return nil, fmt.Errorf("cannot initialize shared folder: %w", err)
	}
	return cfg, nil
}

func icloudCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "icloud", Short: "Recover retained iCloud revisions"}
	var output string
	export := &cobra.Command{Use: "export <revision>", Short: "Decrypt a retained revision to a new recovery file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if output == "" {
			return fmt.Errorf("--output is required; choose a new file outside your Codex home and shared folder")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if cfg.GetStorageConfig().Provider != storage.ProviderICloud {
			return fmt.Errorf("configured provider is not icloud")
		}
		if err = cfg.ValidateICloudPaths(); err != nil {
			return err
		}
		// Treat the output as another local protected path to reject overlap.
		recovery := *cfg
		recovery.EncryptionKey = output
		if err = recovery.ValidateICloudPaths(); err != nil {
			return err
		}
		abs, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		base, err := filepath.EvalSymlinks(config.BaseDir())
		if err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return err
		}
		abs = filepath.Join(parent, filepath.Base(abs))
		rel, err := filepath.Rel(base, abs)
		if err != nil {
			return err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("recovery output must be outside the Codex home")
		}
		store, err := icloud.New(cfg.GetStorageConfig())
		if err != nil {
			return err
		}
		key, encrypted, err := store.(*icloud.Store).ExportRevision(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		enc, err := crypto.NewEncryptor(cfg.EncryptionKey)
		if err != nil {
			return err
		}
		data, err := enc.Decrypt(encrypted)
		if err != nil {
			return err
		}
		if err = writeRecoveryFile(abs, data); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Exported %s to %s (portable path tokens retained). Shared history unchanged.\n", key, abs)
		return nil
	}}
	export.Flags().StringVar(&output, "output", "", "New file outside Codex home and shared folder; never overwritten")
	cmd.AddCommand(export)
	return cmd
}

func writeRecoveryFile(output string, data []byte) error {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer reader.Close()
		const limit = 500 * 1024 * 1024
		data, err = io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return err
		}
		if len(data) > limit {
			return fmt.Errorf("recovery file exceeds decompression limit")
		}
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}

func isICloudStore(store storage.Storage) bool { _, ok := store.(*icloud.Store); return ok }

// Verify a candidate key before replacing the local identity. In particular,
// a cloud hydration failure or mismatched passphrase must not destroy the key
// needed to recover retained revisions. Clearing this backend is never offered.
func deriveICloudKey(ctx context.Context, store storage.Storage, keyPath, passphrase string) error {
	f, err := os.CreateTemp(filepath.Dir(keyPath), ".icloud-key-*")
	if err != nil {
		return err
	}
	candidate := f.Name()
	if err = f.Close(); err != nil {
		return err
	}
	defer os.Remove(candidate)
	if err = crypto.GenerateKeyFromPassphrase(candidate, passphrase); err != nil {
		return err
	}
	if err = verifyKeyMatchesRemote(ctx, store, candidate); err != nil {
		return fmt.Errorf("iCloud key verification failed; original key retained; wait for downloads or retry the correct passphrase: %w", err)
	}
	return os.Rename(candidate, keyPath)
}
