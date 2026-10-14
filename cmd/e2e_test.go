package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/cmd"
	"github.com/h3y6e/skolar/internal/lock"
)

func TestE2ELifecycle(t *testing.T) {
	// Not parallel: overrides IsTTY.
	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func() bool { return false }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// --- Step 1: Create source repo with initial content ---
	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha v1\n",
		"skills/beta/SKILL.md":  "# Beta v1\n",
	})

	// --- Step 2: add --list (no install) ---
	var listOut bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&listOut)
	root.SetArgs([]string{"add", "--list", bareURL})
	if err := root.Execute(); err != nil {
		t.Fatalf("add --list: %v", err)
	}
	got := listOut.String()
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("add --list should show alpha and beta, got: %q", got)
	}
	// No lockfile should exist yet.
	if _, err := os.Stat(lock.FilePath(destDir)); !os.IsNotExist(err) {
		t.Fatal("add --list should not create lockfile")
	}

	// --- Step 3: add (install) ---
	root = cmd.NewRootCmd("test")
	root.SetArgs([]string{"add", "-d", destDir, bareURL})
	if err := root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Verify files exist.
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(destDir, name, "SKILL.md")); err != nil {
			t.Fatalf("%s/SKILL.md should exist after add: %v", name, err)
		}
	}
	// Verify lockfile.
	lf, err := lock.ReadFile(lock.FilePath(destDir))
	if err != nil {
		t.Fatalf("read lockfile after add: %v", err)
	}
	if len(lf.Skills) != 2 {
		t.Fatalf("expected 2 skills in lockfile, got %d", len(lf.Skills))
	}

	// --- Step 4: list (human) ---
	var humanOut bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&humanOut)
	root.SetArgs([]string{"list", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	humanGot := humanOut.String()
	if !strings.Contains(humanGot, "alpha") || !strings.Contains(humanGot, "beta") {
		t.Fatalf("list should show alpha and beta, got: %q", humanGot)
	}

	// --- Step 5: list --json ---
	var jsonOut bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&jsonOut)
	root.SetArgs([]string{"list", "--json", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var listResults []map[string]any
	if err := json.Unmarshal(jsonOut.Bytes(), &listResults); err != nil {
		t.Fatalf("list --json invalid: %v", err)
	}
	if len(listResults) != 2 {
		t.Fatalf("list --json expected 2 entries, got %d", len(listResults))
	}

	// --- Step 6: check (no changes expected) ---
	var checkOut bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&checkOut)
	root.SetArgs([]string{"check", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("check: %v", err)
	}
	checkGot := checkOut.String()
	if !strings.Contains(checkGot, "up-to-date") {
		t.Fatalf("check should show up-to-date, got: %q", checkGot)
	}

	// --- Step 7: check --json ---
	var checkJSON bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&checkJSON)
	root.SetArgs([]string{"check", "--json", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("check --json: %v", err)
	}
	var checkResults []map[string]any
	if err := json.Unmarshal(checkJSON.Bytes(), &checkResults); err != nil {
		t.Fatalf("check --json invalid: %v", err)
	}
	for _, r := range checkResults {
		if r["status"] != "up-to-date" {
			t.Errorf("expected up-to-date status, got %v for %v", r["status"], r["skillName"])
		}
	}

	// --- Step 8: update -y (no changes — should succeed silently) ---
	root = cmd.NewRootCmd("test")
	root.SetArgs([]string{"update", "-y", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("update -y (no changes): %v", err)
	}

	// --- Step 9: remove alpha ---
	root = cmd.NewRootCmd("test")
	root.SetArgs([]string{"remove", "-d", destDir, "alpha"})
	if err := root.Execute(); err != nil {
		t.Fatalf("remove alpha: %v", err)
	}
	// Alpha should be gone.
	if _, err := os.Stat(filepath.Join(destDir, "alpha")); !os.IsNotExist(err) {
		t.Fatal("alpha directory should not exist after remove")
	}
	// Beta should remain.
	if _, err := os.Stat(filepath.Join(destDir, "beta", "SKILL.md")); err != nil {
		t.Fatal("beta should still exist after removing alpha")
	}
	// Lockfile should only have beta.
	lf, err = lock.ReadFile(lock.FilePath(destDir))
	if err != nil {
		t.Fatalf("read lockfile after remove: %v", err)
	}
	if _, ok := lf.Skills["alpha"]; ok {
		t.Error("alpha should not be in lockfile after remove")
	}
	if _, ok := lf.Skills["beta"]; !ok {
		t.Error("beta should still be in lockfile after removing alpha")
	}

	// --- Step 10: rm beta (alias) ---
	root = cmd.NewRootCmd("test")
	root.SetArgs([]string{"rm", "-d", destDir, "beta"})
	if err := root.Execute(); err != nil {
		t.Fatalf("rm beta: %v", err)
	}
	// Beta should be gone.
	if _, err := os.Stat(filepath.Join(destDir, "beta")); !os.IsNotExist(err) {
		t.Fatal("beta directory should not exist after rm")
	}
	// Lockfile should be empty.
	lf, err = lock.ReadFile(lock.FilePath(destDir))
	if err != nil {
		t.Fatalf("read lockfile after rm: %v", err)
	}
	if len(lf.Skills) != 0 {
		t.Errorf("expected 0 skills in lockfile after removing all, got %d", len(lf.Skills))
	}
}

func TestE2EUpdateWithDiff(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha v2\n",
	})

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// Install.
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"add", "-d", destDir, bareURL})
	if err := root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Tamper the installed content to simulate an outdated install.
	os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)

	// Tamper the lockfile hash to simulate a stale entry.
	lockPath := lock.FilePath(destDir)
	lf, _ := lock.ReadFile(lockPath)
	entry := lf.Skills["alpha"]
	entry.ComputedHash = "stale-hash"
	lf.Skills["alpha"] = entry
	lock.WriteFile(lockPath, lf)

	// Check should show update-available.
	var checkOut bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&checkOut)
	root.SetArgs([]string{"check", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut.String(), "update available") {
		t.Fatalf("check should show update available, got: %q", checkOut.String())
	}

	// Update -y should apply.
	root = cmd.NewRootCmd("test")
	root.SetArgs([]string{"update", "-y", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("update -y: %v", err)
	}

	// Verify content was restored to v2.
	data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
	if err != nil {
		t.Fatalf("read alpha after update: %v", err)
	}
	if string(data) != "# Alpha v2\n" {
		t.Errorf("alpha content = %q, want %q", string(data), "# Alpha v2\n")
	}

	// Verify lockfile hash is no longer stale.
	lf, _ = lock.ReadFile(lockPath)
	if lf.Skills["alpha"].ComputedHash == "stale-hash" {
		t.Error("lockfile hash should have been refreshed after update")
	}

	// Check again — should be up-to-date.
	var checkOut2 bytes.Buffer
	root = cmd.NewRootCmd("test")
	root.SetOut(&checkOut2)
	root.SetArgs([]string{"check", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("check after update: %v", err)
	}
	if !strings.Contains(checkOut2.String(), "up-to-date") {
		t.Fatalf("check after update should be up-to-date, got: %q", checkOut2.String())
	}
}
