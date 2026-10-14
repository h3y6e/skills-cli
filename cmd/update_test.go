package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/h3y6e/skills/cmd"
	"github.com/h3y6e/skills/internal/lock"
)

func TestUpdateFlags(t *testing.T) {
	t.Parallel()

	t.Run("--skill flag is registered", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		updateCmd, _, _ := root.Find([]string{"update"})
		flag := updateCmd.Flags().Lookup("skill")
		if flag == nil {
			t.Fatal("expected --skill flag on update command")
		}
	})

	t.Run("-s shorthand works", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		updateCmd, _, _ := root.Find([]string{"update"})
		flag := updateCmd.Flags().ShorthandLookup("s")
		if flag == nil {
			t.Fatal("expected -s shorthand on update command")
		}
	})

	t.Run("--dest flag is registered", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		updateCmd, _, _ := root.Find([]string{"update"})
		flag := updateCmd.Flags().Lookup("dest")
		if flag == nil {
			t.Fatal("expected --dest flag on update command")
		}
	})

	t.Run("--yes flag is registered", func(t *testing.T) {
		t.Parallel()

		root := cmd.NewRootCmd("test")
		updateCmd, _, _ := root.Find([]string{"update"})
		flag := updateCmd.Flags().Lookup("yes")
		if flag == nil {
			t.Fatal("expected --yes flag on update command")
		}
	})
}

func TestUpdateSkillFilter(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("--skill limits output to specified skill only", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install both skills.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Run update with --skill alpha.
		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"update", "-s", "alpha", "-y", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("output should mention alpha, got: %q", got)
		}
		if strings.Contains(got, "beta") {
			t.Errorf("output should not mention beta when --skill alpha, got: %q", got)
		}
	})

	t.Run("updates only skills installed in the selected dest", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		destA := filepath.Join(rootDir, ".agents", "skills")
		destB := filepath.Join(rootDir, ".config", "opencode", "skills")
		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha v2\n",
			"skills/beta/SKILL.md":  "# Beta v2\n",
		})

		if err := os.MkdirAll(filepath.Join(destA, "alpha"), 0o755); err != nil {
			t.Fatalf("mkdir alpha: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destA, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644); err != nil {
			t.Fatalf("write alpha: %v", err)
		}

		lockData := []byte(`{
		  "version": 1,
		  "skills": {
		    "alpha": {
		      "source": ` + strconv.Quote(bareURL) + `,
		      "sourceType": "git",
		      "computedHash": "stale-alpha",
		      "dest": ` + strconv.Quote(destA) + `
		    },
		    "beta": {
		      "source": ` + strconv.Quote(bareURL) + `,
		      "sourceType": "git",
		      "computedHash": "stale-beta",
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

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-y", "-d", destA})
		if err := root.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha: updated") {
			t.Errorf("expected alpha to be updated, got: %q", got)
		}
		if strings.Contains(got, "beta") {
			t.Errorf("did not expect beta in output for dest %q, got: %q", destA, got)
		}

		data, err := os.ReadFile(filepath.Join(destA, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read alpha after update: %v", err)
		}
		if string(data) != "# Alpha v2\n" {
			t.Errorf("alpha content = %q, want %q", string(data), "# Alpha v2\n")
		}
	})

	t.Run("without --skill shows all skills", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install both skills.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"update", "-y", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("output should mention alpha, got: %q", got)
		}
		if !strings.Contains(got, "beta") {
			t.Errorf("output should mention beta, got: %q", got)
		}
	})

	t.Run("reports nothing to update when lockfile is missing", func(t *testing.T) {
		t.Parallel()

		// Arrange: gh-managed destinations have no lockfile at all.
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-y", "-d", destDir})
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if !strings.Contains(out.String(), "no installed skills to update") {
			t.Errorf("output = %q, want %q", out.String(), "no installed skills to update")
		}
	})

	t.Run("detects update-available when lockfile hash differs from upstream", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install skills.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Tamper lockfile hash to simulate outdated install.
		lockPath := lock.FilePath(destDir)
		lf, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		entry := lf.Skills["alpha"]
		entry.ComputedHash = "stale-hash"
		lf.Skills["alpha"] = entry
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"update", "-s", "alpha", "-y", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "updated") {
			t.Errorf("expected 'updated' in output (stale hash should trigger update), got: %q", got)
		}
	})

	t.Run("source argument filters to entries from that source", func(t *testing.T) {
		t.Parallel()

		bareURL2 := initBareRepo(t, map[string]string{
			"skills/gamma/SKILL.md": "# Gamma\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install from first source.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add 1 error: %v", err)
		}

		// Install from second source.
		root2 := cmd.NewRootCmd("test")
		root2.SetArgs([]string{"add", "-d", destDir, bareURL2})
		if err := root2.Execute(); err != nil {
			t.Fatalf("add 2 error: %v", err)
		}

		// Update only from the second source.
		var out bytes.Buffer
		root3 := cmd.NewRootCmd("test")
		root3.SetOut(&out)
		root3.SetArgs([]string{"update", "-y", "-d", destDir, bareURL2})
		if err := root3.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		// Should not mention alpha/beta from first source.
		if strings.Contains(got, "alpha") {
			t.Errorf("output should not mention alpha when source is second repo, got: %q", got)
		}
		if strings.Contains(got, "beta") {
			t.Errorf("output should not mention beta when source is second repo, got: %q", got)
		}
	})

	t.Run("source argument with ref filters to matching ref only", func(t *testing.T) {
		t.Parallel()

		bareURLWithRefs := initBareRepoWithSplitRefSkills(t)
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		if err := os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755); err != nil {
			t.Fatalf("mkdir alpha: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha old\n"), 0o644); err != nil {
			t.Fatalf("write alpha: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(destDir, "beta"), 0o755); err != nil {
			t.Fatalf("mkdir beta: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destDir, "beta", "SKILL.md"), []byte("# Beta old\n"), 0o644); err != nil {
			t.Fatalf("write beta: %v", err)
		}

		lockPath := lock.FilePath(destDir)
		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {
					Source:       bareURLWithRefs,
					SourceType:   "git",
					Ref:          "main",
					ComputedHash: "stale-alpha",
					Dest:         destDir,
				},
				"beta": {
					Source:       bareURLWithRefs,
					SourceType:   "git",
					Ref:          "feature/install",
					ComputedHash: "stale-beta",
					Dest:         destDir,
				},
			},
		}
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-y", "-d", destDir, bareURLWithRefs + "#feature/install"})
		if err := root.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if strings.Contains(got, "alpha") {
			t.Errorf("output should not mention alpha when ref is feature/install, got: %q", got)
		}
		if !strings.Contains(got, "beta: updated") {
			t.Errorf("expected beta to be updated, got: %q", got)
		}

		alphaData, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read alpha: %v", err)
		}
		if string(alphaData) != "# Alpha old\n" {
			t.Errorf("alpha = %q, want %q", string(alphaData), "# Alpha old\n")
		}

		betaData, err := os.ReadFile(filepath.Join(destDir, "beta", "SKILL.md"))
		if err != nil {
			t.Fatalf("read beta: %v", err)
		}
		if string(betaData) != "# Beta feature\n" {
			t.Errorf("beta = %q, want %q", string(betaData), "# Beta feature\n")
		}

		updatedLF, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read updated lockfile: %v", err)
		}
		if updatedLF.Skills["alpha"].ComputedHash != "stale-alpha" {
			t.Errorf("alpha hash = %q, want %q", updatedLF.Skills["alpha"].ComputedHash, "stale-alpha")
		}
		if updatedLF.Skills["beta"].ComputedHash == "stale-beta" {
			t.Error("beta hash should be refreshed")
		}
	})
}

func TestUpdateNoChanges(t *testing.T) {
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

	// Update with no changes upstream — should show up-to-date.
	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"update", "-y", "-d", destDir})
	if err := root2.Execute(); err != nil {
		t.Fatalf("update error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "up-to-date") {
		t.Errorf("expected 'up-to-date' in output, got: %q", got)
	}

	// Verify lockfile and files are unchanged.
	if _, err := os.Stat(filepath.Join(destDir, "alpha", "SKILL.md")); err != nil {
		t.Errorf("alpha/SKILL.md should still exist: %v", err)
	}
}

func TestUpdateApply(t *testing.T) {
	// Not parallel: subtests override package-level IsTTY.

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha v2\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("yes flag applies updates and refreshes lockfile", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Manually install with old content and stale lockfile hash.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)
		os.MkdirAll(filepath.Join(destDir, "beta"), 0o755)
		os.WriteFile(filepath.Join(destDir, "beta", "SKILL.md"), []byte("# Beta\n"), 0o644)

		// Parse the source to get canonical form.
		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-hash-alpha", Dest: destDir},
				"beta":  {Source: bareURL, SourceType: "git", ComputedHash: "stale-hash-beta", Dest: destDir},
			},
		}
		lockPath := lock.FilePath(destDir)
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		// Run update with -y.
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-y", "-d", destDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		// Verify alpha was updated.
		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read alpha SKILL.md: %v", err)
		}
		if string(data) != "# Alpha v2\n" {
			t.Errorf("alpha content = %q, want %q", string(data), "# Alpha v2\n")
		}

		// Verify lockfile hashes are refreshed.
		updatedLF, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read updated lockfile: %v", err)
		}
		alphaEntry := updatedLF.Skills["alpha"]
		if alphaEntry.ComputedHash == "stale-hash-alpha" {
			t.Error("alpha hash should have been updated from stale value")
		}
		if alphaEntry.ComputedHash == "" {
			t.Error("alpha hash should not be empty after update")
		}
	})

	t.Run("non-interactive mode shows preview without applying", func(t *testing.T) {
		// Cannot be parallel: overrides package-level IsTTY.
		origIsTTY := cmd.IsTTY
		cmd.IsTTY = func() bool { return false }
		t.Cleanup(func() { cmd.IsTTY = origIsTTY })

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install with stale hash.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-hash", Dest: destDir},
			},
		}
		lockPath := lock.FilePath(destDir)
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		// Run update WITHOUT -y (non-interactive since tests aren't TTY).
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-d", destDir})
		err := root.Execute()
		// Non-interactive preview should return an error to signal no-apply.
		if err == nil {
			t.Fatal("expected error in non-interactive mode without -y")
		}

		// Files should NOT have been changed.
		data, err2 := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err2 != nil {
			t.Fatalf("read alpha: %v", err2)
		}
		if string(data) != "# Alpha v1\n" {
			t.Errorf("alpha should not have been updated, got: %q", string(data))
		}

		// Lockfile should be unchanged.
		updatedLF, err2 := lock.ReadFile(lockPath)
		if err2 != nil {
			t.Fatalf("read lockfile: %v", err2)
		}
		if updatedLF.Skills["alpha"].ComputedHash != "stale-hash" {
			t.Error("lockfile should not have been modified in non-interactive mode")
		}
	})

	t.Run("yes flag with skill filter applies only filtered skill", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install with stale hashes for both.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)
		os.MkdirAll(filepath.Join(destDir, "beta"), 0o755)
		os.WriteFile(filepath.Join(destDir, "beta", "SKILL.md"), []byte("# Beta old\n"), 0o644)

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-alpha", Dest: destDir},
				"beta":  {Source: bareURL, SourceType: "git", ComputedHash: "stale-beta", Dest: destDir},
			},
		}
		lockPath := lock.FilePath(destDir)
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		// Update only alpha.
		root := cmd.NewRootCmd("test")
		root.SetArgs([]string{"update", "-s", "alpha", "-y", "-d", destDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		// Alpha should be updated.
		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read alpha: %v", err)
		}
		if string(data) != "# Alpha v2\n" {
			t.Errorf("alpha = %q, want %q", string(data), "# Alpha v2\n")
		}

		// Beta should NOT be updated (still old content).
		data, err = os.ReadFile(filepath.Join(destDir, "beta", "SKILL.md"))
		if err != nil {
			t.Fatalf("read beta: %v", err)
		}
		if string(data) != "# Beta old\n" {
			t.Errorf("beta = %q, want %q (should not be updated)", string(data), "# Beta old\n")
		}

		// Lockfile: alpha hash should be updated, beta should remain stale.
		updatedLF, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if updatedLF.Skills["alpha"].ComputedHash == "stale-alpha" {
			t.Error("alpha hash should be refreshed")
		}
		if updatedLF.Skills["beta"].ComputedHash != "stale-beta" {
			t.Error("beta hash should still be stale")
		}
	})
}

func TestUpdateIntegration(t *testing.T) {
	// Not parallel: subtests override package-level IsTTY.

	t.Run("up-to-date skills are not modified during update", func(t *testing.T) {
		t.Parallel()

		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
			"skills/beta/SKILL.md":  "# Beta\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install normally.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Record alpha's mod time before update.
		alphaPath := filepath.Join(destDir, "alpha", "SKILL.md")
		infoBefore, err := os.Stat(alphaPath)
		if err != nil {
			t.Fatalf("stat alpha: %v", err)
		}

		// Run update -y (no changes upstream).
		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"update", "-y", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "up-to-date") {
			t.Errorf("expected 'up-to-date' in output, got: %q", got)
		}
		if strings.Contains(got, "updated") {
			t.Errorf("no skill should have been updated, got: %q", got)
		}

		// Verify file was not touched.
		infoAfter, err := os.Stat(alphaPath)
		if err != nil {
			t.Fatalf("stat alpha after: %v", err)
		}
		if infoBefore.ModTime() != infoAfter.ModTime() {
			t.Error("alpha should not have been modified when up-to-date")
		}
	})

	t.Run("check-failed candidate is reported in update output", func(t *testing.T) {
		t.Parallel()

		// Upstream has only beta, but lockfile references alpha too.
		bareURL := initBareRepo(t, map[string]string{
			"skills/beta/SKILL.md": "# Beta\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Manually install alpha and beta, lockfile references both.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha\n"), 0o644)
		os.MkdirAll(filepath.Join(destDir, "beta"), 0o755)
		os.WriteFile(filepath.Join(destDir, "beta", "SKILL.md"), []byte("# Beta\n"), 0o644)

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "some-hash", Dest: destDir},
				"beta":  {Source: bareURL, SourceType: "git", ComputedHash: "some-hash", Dest: destDir},
			},
		}
		lockPath := lock.FilePath(destDir)
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-y", "-d", destDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "check-failed") {
			t.Errorf("expected 'check-failed' for alpha (removed from upstream), got: %q", got)
		}
		if !strings.Contains(got, "alpha") {
			t.Errorf("expected alpha to be mentioned in output, got: %q", got)
		}
	})

	t.Run("non-interactive preview lists all candidates without modifying files", func(t *testing.T) {
		// Cannot be parallel: overrides IsTTY.
		origIsTTY := cmd.IsTTY
		cmd.IsTTY = func() bool { return false }
		t.Cleanup(func() { cmd.IsTTY = origIsTTY })

		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha v2\n",
			"skills/beta/SKILL.md":  "# Beta v2\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install with stale hashes.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)
		os.MkdirAll(filepath.Join(destDir, "beta"), 0o755)
		os.WriteFile(filepath.Join(destDir, "beta", "SKILL.md"), []byte("# Beta v1\n"), 0o644)

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-a", Dest: destDir},
				"beta":  {Source: bareURL, SourceType: "git", ComputedHash: "stale-b", Dest: destDir},
			},
		}
		lockPath := lock.FilePath(destDir)
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"update", "-d", destDir})
		err := root.Execute()
		if err == nil {
			t.Fatal("expected error in non-interactive mode")
		}

		got := out.String()
		if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
			t.Errorf("preview should list all candidates, got: %q", got)
		}

		// Verify files are unchanged.
		data, _ := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if string(data) != "# Alpha v1\n" {
			t.Error("alpha should not have been modified in preview mode")
		}
		data, _ = os.ReadFile(filepath.Join(destDir, "beta", "SKILL.md"))
		if string(data) != "# Beta v1\n" {
			t.Error("beta should not have been modified in preview mode")
		}

		// Lockfile unchanged.
		updatedLF, _ := lock.ReadFile(lockPath)
		if updatedLF.Skills["alpha"].ComputedHash != "stale-a" {
			t.Error("alpha hash should be unchanged in preview mode")
		}
	})
}
