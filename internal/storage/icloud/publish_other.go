//go:build !darwin && !linux

package icloud

import (
	"fmt"
	"os"
)

func renameRecord(_ *os.Root, _, _ string) error {
	return fmt.Errorf("iCloud publication is supported on macOS only (Linux is supported for testing)")
}
