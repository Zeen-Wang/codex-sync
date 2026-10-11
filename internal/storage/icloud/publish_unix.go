//go:build darwin || linux

package icloud

import (
	"golang.org/x/sys/unix"
	"os"
)

// Use the opened directory descriptor, not its pathname: a concurrently
// replaced parent/symlink must not redirect publication outside the folder.
func renameRecord(r *os.Root, old, new string) error {
	dir, err := r.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = unix.Renameat(int(dir.Fd()), old, int(dir.Fd()), new); err != nil {
		return err
	}
	return dir.Sync()
}
