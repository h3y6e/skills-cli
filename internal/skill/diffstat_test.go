package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/skill"
	"github.com/h3y6e/skolar/internal/testutil"
)

func TestDiffStat(t *testing.T) {
	t.Parallel()

	t.Run("when a lockfile skill updates, files vercel-labs/skills leaves out are not counted", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := t.TempDir()
		testutil.WriteFiles(t, dir, map[string]string{
			"installed/SKILL.md":     "# A\n",
			"upstream/SKILL.md":      "# A v2\n",
			"upstream/metadata.json": "{}\n",
		})
		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, Upstream: os.DirFS(filepath.Join(dir, "upstream"))}

		// Act
		got, err := skill.DiffStat(c, filepath.Join(dir, "installed"))

		// Assert
		if err != nil || got != (skill.Stat{Added: 1, Deleted: 1, Files: 1}) {
			t.Errorf("DiffStat() = %+v, %v, want +1 -1 in 1 file", got, err)
		}
	})

	t.Run("when an update adds an empty file, drops an installed link and only changes a mode, the file and the link count", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := t.TempDir()
		testutil.WriteFiles(t, dir, map[string]string{
			"installed/SKILL.md":   "# A\n",
			"installed/run.sh":     "echo\n",
			"upstream/SKILL.md":    "# A\n",
			"upstream/run.sh":      "echo\n",
			"upstream/__init__.py": "",
		})

		if err := os.Chmod(filepath.Join(dir, "upstream", "run.sh"), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink("SKILL.md", filepath.Join(dir, "installed", "link.md")); err != nil {
			t.Fatal(err)
		}

		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, Upstream: os.DirFS(filepath.Join(dir, "upstream"))}

		// Act
		got, err := skill.DiffStat(c, filepath.Join(dir, "installed"))

		// Assert
		if err != nil || got != (skill.Stat{Files: 2}) {
			t.Errorf("DiffStat() = %+v, %v, want +0 -0 in 2 files", got, err)
		}
	})

	t.Run("when a gh skill updates, the rewritten source metadata is not counted", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := t.TempDir()
		meta := skill.SourceMetadata{RepoURL: "https://github.com/o/r", Ref: "refs/tags/v1.0.0", TreeSHA: "old", Path: "skills/a"}
		writeSkillWithMetadata(t, filepath.Join(dir, "installed"), "a", meta)
		meta.Ref, meta.TreeSHA = "refs/tags/v2.0.0", "new"
		writeSkillWithMetadata(t, filepath.Join(dir, "staged"), "a", meta)
		testutil.WriteFiles(t, dir, map[string]string{"staged/a/references/notes.md": "notes\n"})
		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, Upstream: os.DirFS(filepath.Join(dir, "staged", "a")), Foreign: true}

		// Act
		got, err := skill.DiffStat(c, filepath.Join(dir, "installed", "a"))

		// Assert
		if err != nil || got != (skill.Stat{Added: 1, Files: 1}) {
			t.Errorf("DiffStat() = %+v, %v, want +1 -0 in 1 file", got, err)
		}
	})

	t.Run("when lines move, change at the end of file, or appear twice, lines count as git diff --numstat counts them", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := t.TempDir()
		testutil.WriteFiles(t, dir, map[string]string{
			"installed/SKILL.md": "a\nb\nc\nd\n",
			"installed/tail.md":  "x\ny",
			"upstream/SKILL.md":  "b\nc\nd\na\nb\n",
			"upstream/tail.md":   "x\ny\n",
		})
		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, Upstream: os.DirFS(filepath.Join(dir, "upstream"))}

		// Act
		got, err := skill.DiffStat(c, filepath.Join(dir, "installed"))

		// Assert
		if err != nil || got != (skill.Stat{Added: 3, Deleted: 2, Files: 2}) {
			t.Errorf("DiffStat() = %+v, %v, want +3 -2 in 2 files", got, err)
		}
	})
}
