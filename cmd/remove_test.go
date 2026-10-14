package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/cmd"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
)

func TestRemoveFlags(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test")
	removeCmd, _, _ := root.Find([]string{"remove"})

	t.Run("--dir flag is registered", func(t *testing.T) {
		t.Parallel()
		flag := removeCmd.Flags().Lookup("dir")
		if flag == nil {
			t.Fatal("expected --dir flag on remove command")
		}
	})
}

func TestRemoveAlias(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
	})

	skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// Install.
	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"add", "-d", skillsDir, bareURL})
	if err := root1.Execute(); err != nil {
		t.Fatalf("add error: %v", err)
	}

	// Remove via rm alias.
	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"rm", "-y", "-d", skillsDir, "alpha"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("rm error: %v", err)
	}

	// Skill directory should be gone.
	if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
		t.Error("expected alpha directory to be removed via rm alias")
	}

	// Lockfile should not contain alpha.
	lf, err := lock.ReadFile(lock.FilePath(skillsDir))
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if _, ok := lf.Skills["alpha"]; ok {
		t.Error("expected alpha to be removed from lockfile via rm alias")
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes skill directory and lockfile entry", func(t *testing.T) {
		t.Parallel()

		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
			"skills/beta/SKILL.md":  "# Beta\n",
		})

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install both skills.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", skillsDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Remove only alpha.
		root2 := cmd.NewRootCmd("test")
		root2.SetArgs([]string{"remove", "-y", "-d", skillsDir, "alpha"})
		if err := root2.Execute(); err != nil {
			t.Fatalf("remove error: %v", err)
		}

		// Alpha directory should be gone.
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
			t.Error("expected alpha directory to be removed")
		}

		// Beta directory should still exist.
		if _, err := os.Stat(filepath.Join(skillsDir, "beta", "SKILL.md")); err != nil {
			t.Error("expected beta to remain after removing alpha")
		}

		// Lockfile should only have beta.
		lf, err := lock.ReadFile(lock.FilePath(skillsDir))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if _, ok := lf.Skills["alpha"]; ok {
			t.Error("alpha should not be in lockfile after removal")
		}
		if _, ok := lf.Skills["beta"]; !ok {
			t.Error("beta should still be in lockfile after removing alpha")
		}
	})

	t.Run("when the directory is custom, removing a skill only the lockfile records fails", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		dirA := filepath.Join(rootDir, ".agents", "skills")

		lockData := []byte(`{
		  "version": 1,
		  "skills": {
		    "beta": {
		      "source": "h3y6e/spec-skills",
		      "sourceType": "github",
		      "computedHash": "abc123"
		    }
		  }
		}`)
		if err := os.MkdirAll(filepath.Dir(lock.FilePath(dirA)), 0o755); err != nil {
			t.Fatalf("mkdir lockfile parent: %v", err)
		}
		if err := os.WriteFile(lock.FilePath(dirA), lockData, 0o644); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", filepath.Join(rootDir, "custom", "skills"), "beta"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when removing skill installed in a different dir")
		}
	})

	t.Run("errors when skill not in lockfile", func(t *testing.T) {
		t.Parallel()

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Create a lockfile with one skill.
		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: "h3y6e/spec-skills", SourceType: "github", ComputedHash: "abc123"},
			},
		}
		lock.WriteFile(lock.FilePath(skillsDir), lf)

		// Try to remove a skill that doesn't exist.
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", skillsDir, "nonexistent"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when removing nonexistent skill")
		}
	})

	t.Run("errors when lockfile missing", func(t *testing.T) {
		t.Parallel()

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", skillsDir, "alpha"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when lockfile missing")
		}
	})

	t.Run("when a gh-format skill in a custom directory shares a name with a lockfile entry, removing it keeps the entry", func(t *testing.T) {
		t.Parallel()

		// Arrange: beta is installed in a custom directory in gh skill format,
		// while the lockfile tracks a different beta in the default directory.
		rootDir := t.TempDir()
		dirA := filepath.Join(rootDir, ".agents", "skills")
		custom := filepath.Join(rootDir, "custom", "skills")

		content, err := skill.InjectSourceMetadata("# Beta\n", skill.SourceMetadata{
			RepoURL: "https://github.com/h3y6e/spec-skills",
			Ref:     "refs/tags/v1.0.0",
			TreeSHA: "abc123",
			Path:    "skills/beta",
		})
		if err != nil {
			t.Fatalf("InjectSourceMetadata() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Join(custom, "beta"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(custom, "beta", "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"beta": {Source: "h3y6e/spec-skills", SourceType: "github", ComputedHash: "abc123"},
			},
		}
		if err := lock.WriteFile(lock.FilePath(dirA), lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		// Act
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-y", "-d", custom, "beta"})
		if err := root.Execute(); err != nil {
			t.Fatalf("remove: %v", err)
		}

		// Assert
		if _, err := os.Stat(filepath.Join(custom, "beta")); !errors.Is(err, os.ErrNotExist) {
			t.Error("expected beta directory in the custom directory to be removed")
		}
		updated, err := lock.ReadFile(lock.FilePath(dirA))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if _, ok := updated.Skills["beta"]; !ok {
			t.Errorf("lockfile entry for the default directory must be kept, got %+v", updated)
		}
	})
}

func TestRemoveConfirmation(t *testing.T) {
	// Not parallel: overrides IsTTY.
	origIsTTY := cmd.IsTTY
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	t.Run("when --dry-run is given, removing prints the directory and keeps it", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return false }
		skillsDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, skillsDir, "alpha")
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"remove", "--dry-run", "-d", skillsDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		if !strings.Contains(out.String(), filepath.Join(skillsDir, "alpha")) {
			t.Errorf("output = %q, want the directory to remove", out.String())
		}
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when there is no TTY and no --yes, removing fails and keeps the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return false }
		skillsDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, skillsDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetArgs([]string{"remove", "-d", skillsDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err == nil || !strings.Contains(err.Error(), "confirmation required") {
			t.Fatalf("err = %v, want confirmation required", err)
		}
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when the user declines on a TTY, removing fails and keeps the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return true }
		skillsDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, skillsDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetIn(strings.NewReader("n\n"))
		root.SetArgs([]string{"remove", "-d", skillsDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err == nil {
			t.Fatal("expected an error when declined")
		}
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when canceled while waiting for an answer on a TTY, removing fails and keeps the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return true }
		skillsDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, skillsDir, "alpha")
		stdin, _ := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetIn(stdin)
		root.SetArgs([]string{"remove", "-d", skillsDir, "alpha"})
		cancel()

		// Act
		err := root.ExecuteContext(ctx)

		// Assert
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when the user accepts on a TTY, removing deletes the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return true }
		skillsDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, skillsDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetIn(strings.NewReader("y\n"))
		root.SetArgs([]string{"remove", "-d", skillsDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
			t.Error("alpha should be removed")
		}
	})
}

func writeTrackedSkill(t *testing.T, skillsDir, name string) {
	t.Helper()
	content, err := skill.InjectSourceMetadata("# "+name+"\n", skill.SourceMetadata{
		RepoURL: "https://github.com/h3y6e/spec-skills",
		Ref:     "refs/tags/v1.0.0",
		TreeSHA: "abc123",
		Path:    "skills/" + name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, name, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
