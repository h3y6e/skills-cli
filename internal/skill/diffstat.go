package skill

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/h3y6e/skolar/internal/git"
)

// Stat counts the lines added and deleted, and the files changed, by
// applying an update. Mode changes do not count.
type Stat struct {
	Added   int
	Deleted int
	Files   int
}

// DiffStat counts what applying c changes in the installed skill at dir.
func DiffStat(ctx context.Context, c UpdateCandidate, dir string) (Stat, error) {
	tmp, err := os.MkdirTemp("", "skolar-diffstat-*")
	if err != nil {
		return Stat{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	want, got := c.StagedDir, filepath.Join(tmp, "installed")
	if err := copyDir(dir, got, false); err != nil {
		return Stat{}, err
	}

	if c.Foreign {
		// The installed SKILL.md records the old source metadata; giving it the
		// staged metadata leaves only content changes.
		if err := reinjectMetadata(want, got); err != nil {
			return Stat{}, err
		}
	} else {
		want = filepath.Join(tmp, "staged")
		if err := copyDir(c.StagedDir, want, true); err != nil {
			return Stat{}, err
		}
	}

	added, deleted, err := git.NumStat(ctx, got, want)
	if err != nil {
		return Stat{}, err
	}

	files, err := diffDirs(want, got, false)
	if err != nil {
		return Stat{}, err
	}

	// Update replaces the whole directory, so links, which copies leave out,
	// go too.
	links, err := countLinks(dir)

	return Stat{Added: added, Deleted: deleted, Files: len(files) + links}, err
}

func reinjectMetadata(staged, installed string) error {
	data, err := os.ReadFile(filepath.Join(staged, "SKILL.md"))
	if err != nil {
		return err
	}

	meta, err := ParseSourceMetadata(string(data))
	if err != nil {
		return err
	}

	return injectStagedMetadata(installed, meta)
}

func countLinks(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type()&fs.ModeSymlink != 0 {
			n++
		}

		return err
	})

	return n, err
}
