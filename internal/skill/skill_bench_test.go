package skill_test

import (
	"os"
	"testing"

	"github.com/h3y6e/skolar/internal/skill"
	"github.com/h3y6e/skolar/internal/testutil"
)

func BenchmarkComputeHash(b *testing.B) {
	dir := b.TempDir()
	testutil.WriteFiles(b, dir, map[string]string{
		"SKILL.md": "# Skill\n", "references/a.md": "a\n", "references/b.md": "b\n", "scripts/run.sh": "echo\n", "assets/c.txt": "c\n",
	})

	for b.Loop() {
		if _, err := skill.ComputeHash(os.DirFS(dir)); err != nil {
			b.Fatal(err)
		}
	}
}
