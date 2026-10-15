package skill_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/skill"
	"github.com/h3y6e/skolar/internal/testutil"
)

func BenchmarkDiffStat(b *testing.B) {
	var old, updated strings.Builder

	for i := range 2000 {
		fmt.Fprintf(&old, "line %d of the skill\n", i)

		if i%10 == 0 {
			fmt.Fprintf(&updated, "rewritten line %d\n", i)
		} else {
			fmt.Fprintf(&updated, "line %d of the skill\n", i)
		}
	}

	dir := b.TempDir()
	testutil.WriteFiles(b, dir, map[string]string{"installed/SKILL.md": old.String(), "upstream/SKILL.md": updated.String()})
	c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, Upstream: os.DirFS(filepath.Join(dir, "upstream"))}

	for b.Loop() {
		if _, err := skill.DiffStat(c, filepath.Join(dir, "installed")); err != nil {
			b.Fatal(err)
		}
	}
}
