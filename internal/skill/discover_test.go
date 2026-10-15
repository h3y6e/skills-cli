package skill_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/skill"
)

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func fm(name string) string { return "---\nname: " + name + "\ndescription: d\n---\n" }

var mixedLayouts = map[string]string{
	"skills/alpha/SKILL.md":           fm("alpha"),
	"skills/team/beta/SKILL.md":       fm("beta"),
	"skills/.curated/gamma/SKILL.md":  fm("gamma"),
	".claude/skills/delta/SKILL.md":   fm("delta"),
	"zeta/SKILL.md":                   fm("zeta"),
	"plugins/p/skills/eta/SKILL.md":   fm("eta"),
	"extras/theta/SKILL.md":           fm("theta"),
	".claude-plugin/marketplace.json": `{"plugins": [{"name": "p", "source": "./plugins/p", "skills": ["./skills/eta"]}]}`,
}

func discoveredPaths(t *testing.T, skills []skill.DiscoveredSkill, defaultOnly bool) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, s := range skills {
		if !defaultOnly || s.Default {
			got[s.Name] = s.Path
		}
	}
	return got
}

func TestDiscoverSkillsLayouts(t *testing.T) {
	t.Parallel()

	t.Run("when a repository mixes the layouts either tool supports, every skill is found at its repository path", func(t *testing.T) {
		t.Parallel()
		// Arrange
		files := map[string]string{"SKILL.md": fm("root-skill")}
		maps.Copy(files, mixedLayouts)
		root := writeRepo(t, files)

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"root-skill": ".", "alpha": "skills/alpha", "beta": "skills/team/beta", "gamma": "skills/.curated/gamma",
			"delta": ".claude/skills/delta", "zeta": "zeta", "eta": "plugins/p/skills/eta", "theta": "extras/theta",
		}
		if got := discoveredPaths(t, skills, false); !maps.Equal(got, want) {
			t.Errorf("DiscoverSkills() = %v, want %v", got, want)
		}
	})

	t.Run("when the root holds a SKILL.md, the defaults are the root skill and gh skill's layouts", func(t *testing.T) {
		t.Parallel()
		// Arrange
		files := map[string]string{"SKILL.md": fm("root-skill")}
		maps.Copy(files, mixedLayouts)
		root := writeRepo(t, files)

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"root-skill": ".", "alpha": "skills/alpha", "beta": "skills/team/beta", "zeta": "zeta", "eta": "plugins/p/skills/eta"}
		if got := discoveredPaths(t, skills, true); !maps.Equal(got, want) {
			t.Errorf("defaults = %v, want %v", got, want)
		}
	})

	t.Run("when the root holds no SKILL.md, the defaults follow vercel-labs/skills' search directories", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, mixedLayouts)

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"alpha": "skills/alpha", "beta": "skills/team/beta", "gamma": "skills/.curated/gamma",
			"delta": ".claude/skills/delta", "zeta": "zeta", "eta": "plugins/p/skills/eta",
		}
		if got := discoveredPaths(t, skills, true); !maps.Equal(got, want) {
			t.Errorf("defaults = %v, want %v", got, want)
		}
	})

	t.Run("when two directories declare the same name, the one vercel-labs/skills searches first wins", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{
			"skills/dup/SKILL.md":         fm("dup"),
			".claude/skills/dup/SKILL.md": fm("dup"),
		})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skill.FilterDiscoveredSkills(skills, []string{"dup"}), false); !maps.Equal(got, map[string]string{"dup": "skills/dup"}) {
			t.Errorf("FilterDiscoveredSkills() = %v", got)
		}
	})

	t.Run("when a SKILL.md has no frontmatter name, the skill is named after its directory", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{"skills/plain/SKILL.md": "# Plain\n"})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skills, true); !maps.Equal(got, map[string]string{"plain": "skills/plain"}) {
			t.Errorf("DiscoverSkills() = %v", got)
		}
	})
}

func TestDiscoverSkillsEdges(t *testing.T) {
	t.Run("when a skill directory is named like a skipped directory, it is still found", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{"skills/build/SKILL.md": fm("build"), "skills/build/dist/SKILL.md": fm("hidden")})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skills, false); !maps.Equal(got, map[string]string{"build": "skills/build"}) {
			t.Errorf("DiscoverSkills() = %v", got)
		}
	})

	t.Run("when one SKILL.md has malformed frontmatter, the other skills are still found", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{"skills/good/SKILL.md": fm("good"), "extras/bad/SKILL.md": "---\nname: [\n---\n"})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skills, false); !maps.Equal(got, map[string]string{"good": "skills/good"}) {
			t.Errorf("DiscoverSkills() = %v", got)
		}
	})

	t.Run("when a SKILL.md names a path instead of a skill, it is skipped", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{
			"skills/good/SKILL.md": fm("good"),
			"skills/up/SKILL.md":   fm("../../escape"),
			"skills/abs/SKILL.md":  fm("/tmp/escape"),
			"skills/deep/SKILL.md": fm("a/b"),
			"skills/dot/SKILL.md":  fm("."),
		})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skills, false); !maps.Equal(got, map[string]string{"good": "skills/good"}) {
			t.Errorf("DiscoverSkills() = %v", got)
		}
	})

	t.Run("when a SKILL.md is a symlink, its directory is not a skill", func(t *testing.T) {
		t.Parallel()
		// Arrange
		root := writeRepo(t, map[string]string{"skills/good/SKILL.md": fm("good"), "elsewhere.md": fm("link")})
		if err := os.MkdirAll(filepath.Join(root, "skills", "link"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "elsewhere.md"), filepath.Join(root, "skills", "link", "SKILL.md")); err != nil {
			t.Fatal(err)
		}

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if got := discoveredPaths(t, skills, false); !maps.Equal(got, map[string]string{"good": "skills/good"}) {
			t.Errorf("DiscoverSkills() = %v", got)
		}
	})

	t.Run("when a skill only vercel-labs/skills finds is marked internal, it is found but not a default", func(t *testing.T) {
		// Arrange
		t.Setenv("INSTALL_INTERNAL_SKILLS", "")
		root := writeRepo(t, map[string]string{".claude/skills/secret/SKILL.md": "---\nname: secret\ndescription: d\nmetadata:\n  internal: true\n---\n"})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if len(skills) != 1 || skills[0].Default {
			t.Errorf("skills = %+v, want secret found but not a default", skills)
		}
	})

	t.Run("when INSTALL_INTERNAL_SKILLS opts in, an internal skill is a default", func(t *testing.T) {
		// Arrange
		t.Setenv("INSTALL_INTERNAL_SKILLS", "1")
		root := writeRepo(t, map[string]string{".claude/skills/secret/SKILL.md": "---\nname: secret\ndescription: d\nmetadata:\n  internal: true\n---\n"})

		// Act
		skills, err := skill.DiscoverSkills(root)

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if len(skills) != 1 || !skills[0].Default {
			t.Errorf("skills = %+v, want secret as a default", skills)
		}
	})
}
