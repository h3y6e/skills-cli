package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/h3y6e/skills/internal/skill"
)

// writeFiles creates files for testing.
func writeFiles(t *testing.T, base string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscoverSkills(t *testing.T) {
	t.Parallel()

	t.Run("discovers skills under skills/ directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/my-skill/SKILL.md":        "# My Skill\n",
			"skills/other-skill/SKILL.md":     "# Other\n",
			"skills/other-skill/reference.md": "ref\n",
			"README.md":                       "# Repo\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 2 {
			t.Fatalf("expected 2 skills, got %d", len(skills))
		}

		if skills[0].Name != "my-skill" {
			t.Errorf("skills[0].Name = %q, want %q", skills[0].Name, "my-skill")
		}
		if skills[1].Name != "other-skill" {
			t.Errorf("skills[1].Name = %q, want %q", skills[1].Name, "other-skill")
		}

		for _, s := range skills {
			if s.ComputedHash == "" {
				t.Errorf("skill %q has empty ComputedHash", s.Name)
			}
			if s.Dir == "" {
				t.Errorf("skill %q has empty Dir", s.Name)
			}
		}
	})

	t.Run("falls back to root when no skills/ directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"my-skill/SKILL.md": "# Root Skill\n",
			"README.md":         "# Repo\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(skills))
		}
		if skills[0].Name != "my-skill" {
			t.Errorf("Name = %q, want %q", skills[0].Name, "my-skill")
		}
	})

	t.Run("when skills are namespaced one level deep, discovery finds them by leaf name with full path", func(t *testing.T) {
		t.Parallel()

		// Arrange
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/engineering/codebase-design/SKILL.md": "# Codebase Design\n",
			"skills/writing/writing-japanese/SKILL.md":    "# Writing Japanese\n",
		})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 2 {
			t.Fatalf("expected 2 skills, got %d", len(skills))
		}
		if skills[0].Name != "codebase-design" || skills[0].Path != "skills/engineering/codebase-design" {
			t.Errorf("skills[0] = %q (%q)", skills[0].Name, skills[0].Path)
		}
		if skills[1].Name != "writing-japanese" || skills[1].Path != "skills/writing/writing-japanese" {
			t.Errorf("skills[1] = %q (%q)", skills[1].Name, skills[1].Path)
		}
	})

	t.Run("when flat and namespaced skills coexist, both are discovered", func(t *testing.T) {
		t.Parallel()

		// Arrange
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/cxg/SKILL.md":                         "# cxg\n",
			"skills/engineering/diagnosing-bugs/SKILL.md": "# Diagnosing Bugs\n",
		})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 2 {
			t.Fatalf("expected 2 skills, got %d", len(skills))
		}
		if skills[0].Name != "cxg" || skills[0].Path != "skills/cxg" {
			t.Errorf("skills[0] = %q (%q)", skills[0].Name, skills[0].Path)
		}
		if skills[1].Name != "diagnosing-bugs" || skills[1].Path != "skills/engineering/diagnosing-bugs" {
			t.Errorf("skills[1] = %q (%q)", skills[1].Name, skills[1].Path)
		}
	})

	t.Run("when two namespaces contain the same leaf name, discovery returns an error", func(t *testing.T) {
		t.Parallel()

		// Arrange
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/a/code-review/SKILL.md": "# A\n",
			"skills/b/code-review/SKILL.md": "# B\n",
		})

		// Act
		_, err := skill.DiscoverSkills(root)

		// Assert
		if err == nil {
			t.Fatal("DiscoverSkills() expected error, got nil")
		}
	})

	t.Run("when skills are discovered, Path records the repo-relative directory", func(t *testing.T) {
		t.Parallel()

		// Arrange
		skillsRoot := t.TempDir()
		writeFiles(t, skillsRoot, map[string]string{
			"skills/my-skill/SKILL.md": "# My Skill\n",
		})
		flatRoot := t.TempDir()
		writeFiles(t, flatRoot, map[string]string{
			"my-skill/SKILL.md": "# My Skill\n",
		})

		// Act
		fromSkillsDir, err := skill.DiscoverSkills(skillsRoot)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		fromFlat, err := skill.DiscoverSkills(flatRoot)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}

		// Assert
		if got := fromSkillsDir[0].Path; got != "skills/my-skill" {
			t.Errorf("Path = %q, want %q", got, "skills/my-skill")
		}
		if got := fromFlat[0].Path; got != "my-skill" {
			t.Errorf("Path = %q, want %q", got, "my-skill")
		}
	})

	t.Run("skills/ takes priority over root-level", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/preferred/SKILL.md": "# Preferred\n",
			"root-skill/SKILL.md":       "# Root\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill (skills/ priority), got %d", len(skills))
		}
		if skills[0].Name != "preferred" {
			t.Errorf("Name = %q, want %q", skills[0].Name, "preferred")
		}
	})

	t.Run("ignores directories without SKILL.md", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/valid/SKILL.md":    "# Valid\n",
			"skills/invalid/README.md": "# Not a skill\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(skills))
		}
		if skills[0].Name != "valid" {
			t.Errorf("Name = %q, want %q", skills[0].Name, "valid")
		}
	})

	t.Run("returns empty for repo with no skills", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"README.md": "# Empty Repo\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 0 {
			t.Errorf("expected 0 skills, got %d", len(skills))
		}
	})

	t.Run("does not recurse into nested subdirectories", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/top/SKILL.md":        "# Top\n",
			"skills/top/nested/SKILL.md": "# Nested\n",
		})

		skills, err := skill.DiscoverSkills(root)
		if err != nil {
			t.Fatalf("DiscoverSkills() error = %v", err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill (no recursion), got %d", len(skills))
		}
		if skills[0].Name != "top" {
			t.Errorf("Name = %q, want %q", skills[0].Name, "top")
		}
	})

	t.Run("hash includes all files in skill directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFiles(t, root, map[string]string{
			"skills/s1/SKILL.md": "# S1\n",
		})

		skills1, _ := skill.DiscoverSkills(root)
		hash1 := skills1[0].ComputedHash

		writeFiles(t, root, map[string]string{
			"skills/s1/extra.md": "extra content\n",
		})

		skills2, _ := skill.DiscoverSkills(root)
		hash2 := skills2[0].ComputedHash

		if hash1 == hash2 {
			t.Error("expected hash to change when file added")
		}
	})
}

func TestComputeHashIsDeterministicAndContentSensitive(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"spec-plan/SKILL.md": "# My Skill\n",
	})
	skillPath := filepath.Join(root, "spec-plan")

	first, err := skill.ComputeHash(skillPath)
	if err != nil {
		t.Fatalf("ComputeHash() error = %v", err)
	}

	second, err := skill.ComputeHash(skillPath)
	if err != nil {
		t.Fatalf("ComputeHash() second call error = %v", err)
	}

	if first != second {
		t.Fatalf("ComputeHash() first = %q, second = %q", first, second)
	}

	filePath := filepath.Join(skillPath, "SKILL.md")
	if err := os.WriteFile(filePath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	third, err := skill.ComputeHash(skillPath)
	if err != nil {
		t.Fatalf("ComputeHash() third call error = %v", err)
	}

	if third == first {
		t.Fatalf("ComputeHash() after change = %q, want different from %q", third, first)
	}
}
