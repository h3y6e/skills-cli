package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3y6e/skolar/internal/git"
)

// DiffStat counts what applying c changes in the installed skill at dir.
func DiffStat(ctx context.Context, c UpdateCandidate, dir string) (stat git.Stat, err error) {
	tmp, err := os.MkdirTemp("", "skolar-diffstat-*")
	if err != nil {
		return git.Stat{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	if !c.Foreign {
		staged := filepath.Join(tmp, "staged")
		if err := copyDir(c.StagedDir, staged, true); err != nil {
			return git.Stat{}, err
		}

		return git.DiffStat(ctx, dir, staged)
	}

	// The installed SKILL.md records the old source metadata; giving it the
	// staged metadata leaves only content changes.
	data, err := os.ReadFile(filepath.Join(c.StagedDir, "SKILL.md"))
	if err != nil {
		return git.Stat{}, err
	}

	meta, err := ParseSourceMetadata(string(data))
	if err != nil {
		return git.Stat{}, err
	}

	installed := filepath.Join(tmp, "installed")
	if err := copyDir(dir, installed, false); err != nil {
		return git.Stat{}, err
	}

	if err := injectStagedMetadata(installed, meta); err != nil {
		return git.Stat{}, err
	}

	return git.DiffStat(ctx, installed, c.StagedDir)
}
