package skill_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
)

func TestInstallSkills(t *testing.T) {
	t.Parallel()

	t.Run("copies skill files to destination", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
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

		err := skill.InstallSkills(discovered, ref, lock.NewLayout(skillsDir))
		if err != nil {
			t.Fatalf("InstallSkills() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(skillsDir, "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected installed SKILL.md: %v", err)
		}
		if string(data) != "# My Skill\n" {
			t.Errorf("content = %q, want %q", string(data), "# My Skill\n")
		}

		data2, err := os.ReadFile(filepath.Join(skillsDir, "my-skill", "ref.md"))
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
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, srcDir, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
		})

		discovered, _ := skill.DiscoverSkills(srcDir)
		ref := skill.SourceRef{
			CanonicalSource: "h3y6e/spec-skills",
			SourceType:      "github",
		}

		lockPath := lock.FilePath(skillsDir)
		if err := skill.InstallSkills(discovered, ref, lock.NewLayout(skillsDir)); err != nil {
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
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
		writeFiles(t, srcDir, map[string]string{
			"skills/new-skill/SKILL.md": "# New\n",
		})

		lockPath := lock.FilePath(skillsDir)
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

		if err := skill.InstallSkills(discovered, ref, lock.NewLayout(skillsDir)); err != nil {
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

		skillsDir := t.TempDir()
		err := skill.InstallSkills(nil, skill.SourceRef{}, lock.NewLayout(skillsDir))
		if err == nil {
			t.Fatal("expected error for empty skill list")
		}
	})
}

func TestReplaceSkill(t *testing.T) {
	t.Parallel()

	t.Run("replaces existing skill directory with staged content", func(t *testing.T) {
		t.Parallel()

		skillsDir := t.TempDir()
		stagedDir := t.TempDir()

		writeFiles(t, skillsDir, map[string]string{
			"my-skill/SKILL.md": "# Old\n",
			"my-skill/old-file": "old content\n",
		})

		writeFiles(t, stagedDir, map[string]string{
			"SKILL.md": "# New\n",
			"new-file": "new content\n",
		})

		err := skill.ReplaceSkill("my-skill", stagedDir, lock.NewLayout(skillsDir))
		if err != nil {
			t.Fatalf("ReplaceSkill() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(skillsDir, "my-skill", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if string(data) != "# New\n" {
			t.Errorf("SKILL.md = %q, want %q", string(data), "# New\n")
		}

		data, err = os.ReadFile(filepath.Join(skillsDir, "my-skill", "new-file"))
		if err != nil {
			t.Fatalf("read new-file: %v", err)
		}
		if string(data) != "new content\n" {
			t.Errorf("new-file = %q, want %q", string(data), "new content\n")
		}

		if _, err := os.Stat(filepath.Join(skillsDir, "my-skill", "old-file")); !errors.Is(err, os.ErrNotExist) {
			t.Error("old-file should have been removed")
		}
	})

	t.Run("creates skill directory if it does not exist", func(t *testing.T) {
		t.Parallel()

		skillsDir := t.TempDir()
		stagedDir := t.TempDir()

		writeFiles(t, stagedDir, map[string]string{
			"SKILL.md": "# Brand New\n",
		})

		err := skill.ReplaceSkill("brand-new", stagedDir, lock.NewLayout(skillsDir))
		if err != nil {
			t.Fatalf("ReplaceSkill() error = %v", err)
		}

		data, err := os.ReadFile(filepath.Join(skillsDir, "brand-new", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if string(data) != "# Brand New\n" {
			t.Errorf("SKILL.md = %q, want %q", string(data), "# Brand New\n")
		}
	})
}

func TestInstallRootSkill(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"SKILL.md":            "---\nname: whole\ndescription: d\n---\n",
		"references/notes.md": "notes\n",
		"metadata.json":       "{}\n",
		"__pycache__/x.pyc":   "x\n",
		".git/HEAD":           "ref: refs/heads/main\n",
	}

	t.Run("when a lockfile skill is the repository root, the install leaves out .git and the files vercel-labs/skills skips", func(t *testing.T) {
		t.Parallel()
		// Arrange
		srcDir := t.TempDir()
		writeFiles(t, srcDir, files)
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
		discovered, err := skill.DiscoverSkills(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		// Act
		err = skill.InstallSkills(discovered, skill.SourceRef{CanonicalSource: "o/r", SourceType: "github"}, lock.NewLayout(skillsDir))

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		got := listFiles(t, filepath.Join(skillsDir, "whole"))
		if want := []string{"SKILL.md", "references/notes.md"}; !slices.Equal(got, want) {
			t.Errorf("installed files = %v, want %v", got, want)
		}
	})
}

func TestInstallSkipsSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("when a skill contains a symlink, the install leaves it out instead of copying its target", func(t *testing.T) {
		t.Parallel()
		// Arrange
		srcDir := t.TempDir()
		writeFiles(t, srcDir, map[string]string{"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\n"})
		secret := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(secret, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(srcDir, "skills", "s", "leak")); err != nil {
			t.Fatal(err)
		}
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
		discovered, err := skill.DiscoverSkills(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		// Act
		err = skill.InstallSkills(discovered, skill.SourceRef{CanonicalSource: "o/r", SourceType: "github"}, lock.NewLayout(skillsDir))

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := listFiles(t, filepath.Join(skillsDir, "s")); !slices.Equal(got, []string{"SKILL.md"}) {
			t.Errorf("installed files = %v, want [SKILL.md]", got)
		}
	})
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		files = append(files, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}
