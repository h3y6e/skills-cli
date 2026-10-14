package cmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/cmd"
	"github.com/h3y6e/skolar/internal/lock"
)

func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	os.Exit(m.Run())
}

// initBareRepo creates a bare git repo with the given files (2 commits for
// shallow clone compatibility). Returns a file:// URL.
func initBareRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	work := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@test.com")
	gitRun("config", "user.name", "Test")

	os.WriteFile(filepath.Join(work, ".gitkeep"), []byte(""), 0o644)
	gitRun("add", "-A")
	gitRun("commit", "-m", "base")

	for name, content := range files {
		p := filepath.Join(work, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	gitRun("add", "-A")
	gitRun("commit", "-m", "add files")

	bare := t.TempDir()
	c := exec.Command("git", "clone", "--bare", work, bare)
	c.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
	return "file://" + bare
}

func initBareRepoWithRefs(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@test.com")
	gitRun("config", "user.name", "Test")

	skillPath := filepath.Join(work, "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# Alpha main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "-A")
	gitRun("commit", "-m", "main")
	gitRun("tag", "v1.0.0")

	gitRun("checkout", "-b", "feature/install")
	if err := os.WriteFile(skillPath, []byte("# Alpha feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "-A")
	gitRun("commit", "-m", "feature")
	gitRun("checkout", "main")

	bare := t.TempDir()
	c := exec.Command("git", "clone", "--bare", work, bare)
	c.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
	return "file://" + bare
}

func initBareRepoWithSplitRefSkills(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@test.com")
	gitRun("config", "user.name", "Test")

	alphaPath := filepath.Join(work, "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(alphaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alphaPath, []byte("# Alpha main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "-A")
	gitRun("commit", "-m", "main")

	gitRun("checkout", "-b", "feature/install")
	betaPath := filepath.Join(work, "skills", "beta", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(betaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(betaPath, []byte("# Beta feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "-A")
	gitRun("commit", "-m", "feature")
	gitRun("checkout", "main")

	bare := t.TempDir()
	c := exec.Command("git", "clone", "--bare", work, bare)
	c.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
	return "file://" + bare
}

func TestAddList(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("lists discovered skills", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"add", "--list", bareURL})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("output should contain 'alpha', got: %q", got)
		}
		if !strings.Contains(got, "beta") {
			t.Errorf("output should contain 'beta', got: %q", got)
		}
	})

	t.Run("requires source argument", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "--list"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when no source provided")
		}
	})
}

func TestAddSkillFlag(t *testing.T) {
	t.Parallel()

	t.Run("--skill flag is registered", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		addCmd, _, _ := root.Find([]string{"add"})
		flag := addCmd.Flags().Lookup("skill")
		if flag == nil {
			t.Fatal("expected --skill flag on add command")
		}
	})

	t.Run("-s shorthand works", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		addCmd, _, _ := root.Find([]string{"add"})
		flag := addCmd.Flags().ShorthandLookup("s")
		if flag == nil {
			t.Fatal("expected -s shorthand on add command")
		}
	})
}

func TestAddInstall(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
		"skills/alpha/ref.md":   "alpha ref\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("installs selected skill with --skill flag", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "-s", "alpha", "-d", destDir, bareURL})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}

		// Verify skill files installed.
		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected alpha/SKILL.md: %v", err)
		}
		if string(data) != "# Alpha\n" {
			t.Errorf("content = %q, want %q", string(data), "# Alpha\n")
		}

		data, err = os.ReadFile(filepath.Join(destDir, "alpha", "ref.md"))
		if err != nil {
			t.Fatalf("expected alpha/ref.md: %v", err)
		}
		if string(data) != "alpha ref\n" {
			t.Errorf("content = %q, want %q", string(data), "alpha ref\n")
		}

		// Beta should NOT be installed.
		if _, err := os.Stat(filepath.Join(destDir, "beta")); !os.IsNotExist(err) {
			t.Error("beta should not be installed when --skill alpha is specified")
		}

		// Verify lockfile exists and has correct entry.
		lockData, err := os.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("expected skills-lock.json: %v", err)
		}
		lockStr := string(lockData)
		if !strings.Contains(lockStr, `"alpha"`) {
			t.Errorf("lockfile should contain alpha entry, got: %s", lockStr)
		}
		if !strings.Contains(lockStr, `"dest": `) {
			t.Errorf("lockfile should persist dest for installed skills, got: %s", lockStr)
		}
		if strings.Contains(lockStr, `"beta"`) {
			t.Errorf("lockfile should not contain beta entry, got: %s", lockStr)
		}
	})

	t.Run("installs all skills without --skill flag", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}

		// Both skills should be installed.
		if _, err := os.Stat(filepath.Join(destDir, "alpha", "SKILL.md")); err != nil {
			t.Errorf("expected alpha/SKILL.md: %v", err)
		}
		if _, err := os.Stat(filepath.Join(destDir, "beta", "SKILL.md")); err != nil {
			t.Errorf("expected beta/SKILL.md: %v", err)
		}

		lockData, err := os.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("expected skills-lock.json: %v", err)
		}
		lockStr := string(lockData)
		if !strings.Contains(lockStr, `"alpha"`) {
			t.Error("lockfile should contain alpha")
		}
		if !strings.Contains(lockStr, `"beta"`) {
			t.Error("lockfile should contain beta")
		}
	})

	t.Run("persists ref when source uses ref fragment", func(t *testing.T) {
		t.Parallel()

		bareURLWithRefs := initBareRepoWithRefs(t)
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "-d", destDir, bareURLWithRefs + "#feature/install"})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("expected alpha/SKILL.md: %v", err)
		}
		if string(data) != "# Alpha feature\n" {
			t.Errorf("content = %q, want %q", string(data), "# Alpha feature\n")
		}

		lf, err := lock.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if lf.Skills["alpha"].Ref != "feature/install" {
			t.Errorf("alpha.Ref = %q, want %q", lf.Skills["alpha"].Ref, "feature/install")
		}
	})

	t.Run("merges lockfile on second add from different source", func(t *testing.T) {
		t.Parallel()

		bareURL2 := initBareRepo(t, map[string]string{
			"skills/gamma/SKILL.md": "# Gamma\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// First add.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-s", "alpha", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("first add error: %v", err)
		}

		// Second add from different source.
		root2 := cmd.NewRootCmd("test")
		root2.SetArgs([]string{"add", "-d", destDir, bareURL2})
		if err := root2.Execute(); err != nil {
			t.Fatalf("second add error: %v", err)
		}

		// Both alpha and gamma should exist.
		if _, err := os.Stat(filepath.Join(destDir, "alpha", "SKILL.md")); err != nil {
			t.Error("expected alpha to still be installed")
		}
		if _, err := os.Stat(filepath.Join(destDir, "gamma", "SKILL.md")); err != nil {
			t.Error("expected gamma to be installed")
		}

		lockData, err := os.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("expected skills-lock.json: %v", err)
		}
		lockStr := string(lockData)
		if !strings.Contains(lockStr, `"alpha"`) {
			t.Error("lockfile should still contain alpha")
		}
		if !strings.Contains(lockStr, `"gamma"`) {
			t.Error("lockfile should contain gamma")
		}
	})

	t.Run("errors when --skill specifies nonexistent skill", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "-s", "nonexistent", "-d", destDir, bareURL})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error for nonexistent skill")
		}
		if !strings.Contains(err.Error(), "no skills found") {
			t.Errorf("error = %q, want containing 'no skills found'", err.Error())
		}
	})

	t.Run("does not add dest to existing entries that lack it", func(t *testing.T) {
		t.Parallel()

		bareURL2 := initBareRepo(t, map[string]string{
			"skills/delta/SKILL.md": "# Delta\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Seed a lockfile with an entry that has no dest field.
		seedLF := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "abc123"},
			},
		}
		if err := lock.WriteFile(lock.FilePath(destDir), seedLF); err != nil {
			t.Fatalf("write seed lockfile: %v", err)
		}
		// Pre-install alpha directory so the lockfile is consistent.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha\n"), 0o644)

		// Add delta from a different source.
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"add", "-d", destDir, bareURL2})
		if err := root.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Read back lockfile and verify alpha still has no dest.
		updatedLF, err := lock.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		alphaEntry := updatedLF.Skills["alpha"]
		if alphaEntry.Dest != "" {
			t.Errorf("alpha.Dest should remain empty after adding unrelated skill, got %q", alphaEntry.Dest)
		}

		// delta should have dest set.
		deltaEntry := updatedLF.Skills["delta"]
		if deltaEntry.Dest == "" {
			t.Error("delta.Dest should be set for newly added skill")
		}
	})
}
