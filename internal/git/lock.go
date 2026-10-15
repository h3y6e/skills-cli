package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lock takes an exclusive lock on path, waiting for other processes and
// goroutines to release it until ctx is done.
func lock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}

	for {
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("lock cache: %w", err)
		}

		if locked {
			return func() { _ = f.Close() }, nil
		}

		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, context.Cause(ctx))
		case <-time.After(50 * time.Millisecond):
		}
	}
}
