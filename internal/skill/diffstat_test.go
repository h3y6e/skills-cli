package skill_test

import (
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
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
		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, StagedDir: filepath.Join(dir, "upstream")}

		// Act
		got, err := skill.DiffStat(t.Context(), c, filepath.Join(dir, "installed"))

		// Assert
		if err != nil || got != (git.Stat{Added: 1, Deleted: 1, Files: 1}) {
			t.Errorf("DiffStat() = %+v, %v, want +1 -1 in 1 file", got, err)
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
		c := skill.UpdateCandidate{Status: skill.StatusUpdateAvailable, StagedDir: filepath.Join(dir, "staged", "a"), Foreign: true}

		// Act
		got, err := skill.DiffStat(t.Context(), c, filepath.Join(dir, "installed", "a"))

		// Assert
		if err != nil || got != (git.Stat{Added: 1, Files: 1}) {
			t.Errorf("DiffStat() = %+v, %v, want +1 -0 in 1 file", got, err)
		}
	})
}
