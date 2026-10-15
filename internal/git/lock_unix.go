//go:build unix

package git

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lock takes an exclusive lock on path, waiting for other processes and
// goroutines to release it.
func lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock cache: %w", err)
	}

	return func() { _ = f.Close() }, nil
}
