package cmd_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

	t.Run("--dest flag is registered", func(t *testing.T) {
		t.Parallel()
		flag := removeCmd.Flags().Lookup("dest")
		if flag == nil {
			t.Fatal("expected --dest flag on remove command")
		}
	})
}

func TestRemoveAlias(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
	})

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// Install.
	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"add", "-d", destDir, bareURL})
	if err := root1.Execute(); err != nil {
		t.Fatalf("add error: %v", err)
	}

	// Remove via rm alias.
	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"rm", "-y", "-d", destDir, "alpha"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("rm error: %v", err)
	}

	// Skill directory should be gone.
	if _, err := os.Stat(filepath.Join(destDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
		t.Error("expected alpha directory to be removed via rm alias")
	}

	// Lockfile should not contain alpha.
	lf, err := lock.ReadFile(lock.FilePath(destDir))
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

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install both skills.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Remove only alpha.
		root2 := cmd.NewRootCmd("test")
		root2.SetArgs([]string{"remove", "-y", "-d", destDir, "alpha"})
		if err := root2.Execute(); err != nil {
			t.Fatalf("remove error: %v", err)
		}

		// Alpha directory should be gone.
		if _, err := os.Stat(filepath.Join(destDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
			t.Error("expected alpha directory to be removed")
		}

		// Beta directory should still exist.
		if _, err := os.Stat(filepath.Join(destDir, "beta", "SKILL.md")); err != nil {
			t.Error("expected beta to remain after removing alpha")
		}

		// Lockfile should only have beta.
		lf, err := lock.ReadFile(lock.FilePath(destDir))
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

	t.Run("errors when skill exists in a different dest", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		destA := filepath.Join(rootDir, ".agents", "skills")
		destB := filepath.Join(rootDir, ".config", "opencode", "skills")

		lockData := []byte(`{
		  "version": 1,
		  "skills": {
		    "beta": {
		      "source": "h3y6e/spec-skills",
		      "sourceType": "github",
		      "computedHash": "abc123",
		      "dest": ` + strconv.Quote(destB) + `
		    }
		  }
		}`)
		if err := os.MkdirAll(filepath.Dir(lock.FilePath(destA)), 0o755); err != nil {
			t.Fatalf("mkdir lockfile parent: %v", err)
		}
		if err := os.WriteFile(lock.FilePath(destA), lockData, 0o644); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", destA, "beta"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when removing skill installed in a different dest")
		}
	})

	t.Run("errors when skill not in lockfile", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Create a lockfile with one skill.
		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: "h3y6e/spec-skills", SourceType: "github", ComputedHash: "abc123"},
			},
		}
		lock.WriteFile(lock.FilePath(destDir), lf)

		// Try to remove a skill that doesn't exist.
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", destDir, "nonexistent"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when removing nonexistent skill")
		}
	})

	t.Run("errors when lockfile missing", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-d", destDir, "alpha"})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error when lockfile missing")
		}
	})

	t.Run("when a tracked skill shares a name with a lock entry for a different dest, removing keeps the other entry", func(t *testing.T) {
		t.Parallel()

		// Arrange: beta is installed in destA in gh skill format, while the
		// lockfile tracks a different beta in destB.
		rootDir := t.TempDir()
		destA := filepath.Join(rootDir, ".agents", "skills")
		destB := filepath.Join(rootDir, ".config", "opencode", "skills")

		content, err := skill.InjectSourceMetadata("# Beta\n", skill.SourceMetadata{
			RepoURL: "https://github.com/h3y6e/spec-skills",
			Ref:     "refs/tags/v1.0.0",
			TreeSHA: "abc123",
			Path:    "skills/beta",
		})
		if err != nil {
			t.Fatalf("InjectSourceMetadata() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Join(destA, "beta"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destA, "beta", "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"beta": {Source: "h3y6e/spec-skills", SourceType: "github", ComputedHash: "abc123", Dest: destB},
			},
		}
		if err := lock.WriteFile(lock.FilePath(destA), lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		// Act
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"remove", "-y", "-d", destA, "beta"})
		if err := root.Execute(); err != nil {
			t.Fatalf("remove: %v", err)
		}

		// Assert
		if _, err := os.Stat(filepath.Join(destA, "beta")); !errors.Is(err, os.ErrNotExist) {
			t.Error("expected beta directory in destA to be removed")
		}
		updated, err := lock.ReadFile(lock.FilePath(destA))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		entry, ok := updated.Skills["beta"]
		if !ok {
			t.Fatal("lock entry for destB must not be removed")
		}
		if entry.Dest != destB {
			t.Errorf("entry.Dest = %q, want %q", entry.Dest, destB)
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
		destDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, destDir, "alpha")
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"remove", "--dry-run", "-d", destDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		if !strings.Contains(out.String(), filepath.Join(destDir, "alpha")) {
			t.Errorf("output = %q, want the directory to remove", out.String())
		}
		if _, err := os.Stat(filepath.Join(destDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when there is no TTY and no --yes, removing fails and keeps the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return false }
		destDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, destDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetArgs([]string{"remove", "-d", destDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err == nil || !strings.Contains(err.Error(), "confirmation required") {
			t.Fatalf("err = %v, want confirmation required", err)
		}
		if _, err := os.Stat(filepath.Join(destDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when the user declines on a TTY, removing fails and keeps the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return true }
		destDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, destDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetIn(strings.NewReader("n\n"))
		root.SetArgs([]string{"remove", "-d", destDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err == nil {
			t.Fatal("expected an error when declined")
		}
		if _, err := os.Stat(filepath.Join(destDir, "alpha")); err != nil {
			t.Error("alpha should remain")
		}
	})

	t.Run("when the user accepts on a TTY, removing deletes the skill", func(t *testing.T) {
		// Arrange
		cmd.IsTTY = func() bool { return true }
		destDir := filepath.Join(t.TempDir(), "skills")
		writeTrackedSkill(t, destDir, "alpha")
		root := cmd.NewRootCmd("test")
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetIn(strings.NewReader("y\n"))
		root.SetArgs([]string{"remove", "-d", destDir, "alpha"})

		// Act
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		if _, err := os.Stat(filepath.Join(destDir, "alpha")); !errors.Is(err, os.ErrNotExist) {
			t.Error("alpha should be removed")
		}
	})
}

func writeTrackedSkill(t *testing.T, destDir, name string) {
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
	if err := os.MkdirAll(filepath.Join(destDir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, name, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
