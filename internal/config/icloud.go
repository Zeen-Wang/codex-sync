package config

import (
	"fmt"
	"github.com/d-jiao/codex-sync/internal/storage"
	"os"
	"path/filepath"
	"strings"
)

// ValidateICloudPaths keeps local identity, keys and state out of the transport,
// including when an ancestor is a symlink. The shared folder must already exist.
func (c *Config) ValidateICloudPaths() error {
	if c.GetStorageConfig().Provider != storage.ProviderICloud {
		return nil
	}
	folder, err := filepath.EvalSymlinks(c.Storage.SharedFolder)
	if err != nil {
		return fmt.Errorf("shared folder unavailable: %w", err)
	}
	base := c.BaseDirOverride
	if base == "" {
		base = BaseDir()
	}
	state := c.StateDirOverride
	if state == "" {
		state = ConfigDirPath()
	}
	for _, p := range []string{base, state, c.EncryptionKey} {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			home, e := os.UserHomeDir()
			if e != nil {
				return e
			}
			p = filepath.Join(home, p[2:])
		}
		p, err = canonicalPath(p)
		if err != nil {
			return err
		}
		if within(folder, p) || within(p, folder) {
			return fmt.Errorf("iCloud shared folder must not overlap Codex home, local sync state, or encryption key: %s", p)
		}
	}
	return nil
}
func within(parent, child string) bool {
	r, e := filepath.Rel(parent, child)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
func canonicalPath(p string) (string, error) {
	p, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e == nil {
		return resolved, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", e
	}
	resolved, e = canonicalPath(parent)
	if e != nil {
		return "", e
	}
	return filepath.Join(resolved, filepath.Base(p)), nil
}
