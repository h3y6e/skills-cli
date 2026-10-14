package skill_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
)

func TestInstallSkills(t *testing.T) {
	t.Parallel()

	t.Run("copies skill files to destination", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, srcDir, map[string]string{
			"skills/my-skill/SKILL.md": "# My Skill\n",
			"skills/my-skill/ref.md":   "reference\n",
		})

		discovered, _ := skill.DiscoverSkills(srcDir)

		ref := skill.SourceRef{
			Raw:             "h3y6e/spec-skills",
			SourceType:      "github",
			CanonicalSource: "h3y6e/spec-skills",
			CloneURL:        "https://github.com/h3y6e/spec-skills.git",
		}

		err := skill.InstallSkills(discovered, ref, lock.NewLayout(destDir))
		if err != nil {
			t.Fatalf("InstallSkills() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDir, "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected installed SKILL.md: %v", err)
		}
		if string(data) != "# My Skill\n" {
			t.Errorf("content = %q, want %q", string(data), "# My Skill\n")
		}

		data2, err := os.ReadFile(filepath.Join(destDir, "my-skill", "ref.md"))
		if err != nil {
			t.Fatalf("expected installed ref.md: %v", err)
		}
		if string(data2) != "reference\n" {
			t.Errorf("content = %q, want %q", string(data2), "reference\n")
		}
	})

	t.Run("creates lockfile with entries", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, srcDir, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
		})

		discovered, _ := skill.DiscoverSkills(srcDir)
		ref := skill.SourceRef{
			CanonicalSource: "h3y6e/spec-skills",
			SourceType:      "github",
		}

		lockPath := lock.FilePath(destDir)
		if err := skill.InstallSkills(discovered, ref, lock.NewLayout(destDir)); err != nil {
			t.Fatalf("InstallSkills() error = %v", err)
		}

		lf, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if lf.Version != 1 {
			t.Errorf("version = %d, want 1", lf.Version)
		}

		entry, ok := lf.Skills["alpha"]
		if !ok {
			t.Fatal("expected lockfile entry for 'alpha'")
		}
		if entry.Source != "h3y6e/spec-skills" {
			t.Errorf("Source = %q, want %q", entry.Source, "h3y6e/spec-skills")
		}
		if entry.SourceType != "github" {
			t.Errorf("SourceType = %q, want %q", entry.SourceType, "github")
		}
		if entry.ComputedHash == "" {
			t.Error("expected non-empty ComputedHash")
		}
	})

	t.Run("merges into existing lockfile", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, srcDir, map[string]string{
			"skills/new-skill/SKILL.md": "# New\n",
		})

		lockPath := lock.FilePath(destDir)
		existing := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"old-skill": {
					Source:       "other/repo",
					SourceType:   "github",
					ComputedHash: "existing-hash",
				},
			},
		}
		if err := lock.WriteFile(lockPath, existing); err != nil {
			t.Fatalf("write existing lockfile: %v", err)
		}

		discovered, _ := skill.DiscoverSkills(srcDir)
		ref := skill.SourceRef{
			CanonicalSource: "h3y6e/spec-skills",
			SourceType:      "github",
		}

		if err := skill.InstallSkills(discovered, ref, lock.NewLayout(destDir)); err != nil {
			t.Fatalf("InstallSkills() error = %v", err)
		}

		lf, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}

		if _, ok := lf.Skills["old-skill"]; !ok {
			t.Error("expected old-skill entry to be preserved")
		}
		if _, ok := lf.Skills["new-skill"]; !ok {
			t.Error("expected new-skill entry to be added")
		}
	})

	t.Run("does not corrupt destination on empty input", func(t *testing.T) {
		t.Parallel()

		destDir := t.TempDir()
		err := skill.InstallSkills(nil, skill.SourceRef{}, lock.NewLayout(destDir))
		if err == nil {
			t.Fatal("expected error for empty skill list")
		}
	})
}

func TestReplaceSkill(t *testing.T) {
	t.Parallel()

	t.Run("replaces existing skill directory with staged content", func(t *testing.T) {
		t.Parallel()

		destDir := t.TempDir()
		stagedDir := t.TempDir()

		writeFiles(t, destDir, map[string]string{
			"my-skill/SKILL.md": "# Old\n",
			"my-skill/old-file": "old content\n",
		})

		writeFiles(t, stagedDir, map[string]string{
			"SKILL.md": "# New\n",
			"new-file": "new content\n",
		})

		err := skill.ReplaceSkill("my-skill", stagedDir, lock.NewLayout(destDir))
		if err != nil {
			t.Fatalf("ReplaceSkill() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDir, "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if string(data) != "# New\n" {
			t.Errorf("SKILL.md = %q, want %q", string(data), "# New\n")
		}

		data, err = os.ReadFile(filepath.Join(destDir, "my-skill", "new-file"))
		if err != nil {
			t.Fatalf("read new-file: %v", err)
		}
		if string(data) != "new content\n" {
			t.Errorf("new-file = %q, want %q", string(data), "new content\n")
		}

		if _, err := os.Stat(filepath.Join(destDir, "my-skill", "old-file")); !errors.Is(err, os.ErrNotExist) {
			t.Error("old-file should have been removed")
		}
	})

	t.Run("creates skill directory if it does not exist", func(t *testing.T) {
		t.Parallel()

		destDir := t.TempDir()
		stagedDir := t.TempDir()

		writeFiles(t, stagedDir, map[string]string{
			"SKILL.md": "# Brand New\n",
		})

		err := skill.ReplaceSkill("brand-new", stagedDir, lock.NewLayout(destDir))
		if err != nil {
			t.Fatalf("ReplaceSkill() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDir, "brand-new", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if string(data) != "# Brand New\n" {
			t.Errorf("SKILL.md = %q, want %q", string(data), "# Brand New\n")
		}
	})
}
