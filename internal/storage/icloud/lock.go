package icloud

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Lock serializes sync operations on this Mac only. The lock is outside iCloud:
// replicating a lock file cannot provide exclusion between offline devices.
// An interrupted process leaves a lock that requires explicit manual recovery.
func (s *Store) Lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	folder, err := filepath.EvalSymlinks(s.folder)
	if err != nil {
		return nil, err
	}
	lock := filepath.Join(os.TempDir(), "codex-sync-icloud-"+digest([]byte(folder))+".lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return nil, fmt.Errorf("iCloud sync is locked locally (%s); if no codex-sync process is running, remove this stale directory: %w", lock, err)
	}
	return func() { _ = os.Remove(lock) }, nil
}
