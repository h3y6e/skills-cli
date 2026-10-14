package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/cmd"
	"github.com/h3y6e/skolar/internal/lock"
)

func TestCheckFlags(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test")
	checkCmd, _, _ := root.Find([]string{"check"})

	t.Run("--json flag is registered", func(t *testing.T) {
		t.Parallel()
		flag := checkCmd.Flags().Lookup("json")
		if flag == nil {
			t.Fatal("expected --json flag on check command")
		}
	})
}

func TestCheckHuman(t *testing.T) {
	t.Parallel()

	t.Run("shows up-to-date when no changes", func(t *testing.T) {
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

		// Check.
		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("check error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "up-to-date") {
			t.Errorf("expected 'up-to-date', got: %q", got)
		}
	})

	t.Run("shows update-available with stale hash", func(t *testing.T) {
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

		// Tamper hash.
		lockPath := lock.FilePath(destDir)
		lf, _ := lock.ReadFile(lockPath)
		entry := lf.Skills["alpha"]
		entry.ComputedHash = "stale-hash"
		lf.Skills["alpha"] = entry
		lock.WriteFile(lockPath, lf)

		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("check error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "update available") {
			t.Errorf("expected 'update available', got: %q", got)
		}
	})

	t.Run("does not contain diff output", func(t *testing.T) {
		t.Parallel()

		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha v2\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Manually install old content with stale hash.
		os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
		os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale", Dest: destDir},
			},
		}
		lock.WriteFile(lock.FilePath(destDir), lf)

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"check", "-d", destDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("check error: %v", err)
		}

		got := out.String()
		// Check should NOT include diff/patch content.
		if strings.Contains(got, "diff --git") {
			t.Errorf("check should not contain diff output, got: %q", got)
		}
		if strings.Contains(got, "@@") {
			t.Errorf("check should not contain patch markers, got: %q", got)
		}
	})

	t.Run("checks only skills installed in the selected dest", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		destA := filepath.Join(rootDir, ".agents", "skills")
		destB := filepath.Join(rootDir, ".config", "opencode", "skills")
		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
			"skills/beta/SKILL.md":  "# Beta\n",
		})

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
		root.SetArgs([]string{"check", "-d", destA})
		if err := root.Execute(); err != nil {
			t.Fatalf("check error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("expected alpha in output, got: %q", got)
		}
		if strings.Contains(got, "beta") {
			t.Errorf("did not expect beta in output for dest %q, got: %q", destA, got)
		}
	})

	t.Run("reports nothing to check when lockfile is missing", func(t *testing.T) {
		t.Parallel()

		// Arrange: gh-managed destinations have no lockfile at all.
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetErr(&out)
		root.SetArgs([]string{"check", "-d", destDir})
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if !strings.Contains(out.String(), "no installed skills to check") {
			t.Errorf("output = %q, want %q", out.String(), "no installed skills to check")
		}
	})
}

func TestCheckJSON(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("outputs valid JSON array", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Tamper one hash.
		lockPath := lock.FilePath(destDir)
		lf, _ := lock.ReadFile(lockPath)
		entry := lf.Skills["alpha"]
		entry.ComputedHash = "stale"
		lf.Skills["alpha"] = entry
		lock.WriteFile(lockPath, lf)

		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "--json", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("check --json error: %v", err)
		}

		results := decodeRecord(t, out.Bytes(), "results")

		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		// Find alpha and beta in results.
		var alphaResult, betaResult map[string]any
		for _, r := range results {
			switch r["skillName"] {
			case "alpha":
				alphaResult = r
			case "beta":
				betaResult = r
			}
		}

		if alphaResult == nil {
			t.Fatal("alpha not found in JSON results")
		}
		if betaResult == nil {
			t.Fatal("beta not found in JSON results")
		}

		if alphaResult["status"] != "update-available" {
			t.Errorf("alpha status = %v, want update-available", alphaResult["status"])
		}
		if betaResult["status"] != "up-to-date" {
			t.Errorf("beta status = %v, want up-to-date", betaResult["status"])
		}

		// JSON should have required fields.
		for _, field := range []string{"skillName", "status", "source", "currentHash"} {
			if _, ok := alphaResult[field]; !ok {
				t.Errorf("alpha missing required JSON field %q", field)
			}
		}
	})
}

func TestCheckNonMutating(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha v2\n",
	})

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// Manually install old version with stale hash.
	os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755)
	os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte("# Alpha v1\n"), 0o644)

	lf := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-hash", Dest: destDir},
		},
	}
	lockPath := lock.FilePath(destDir)
	lock.WriteFile(lockPath, lf)

	// Record lockfile content before check.
	lockBefore, _ := os.ReadFile(lockPath)
	fileBefore, _ := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))

	// Run check (human mode).
	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"check", "-d", destDir})
	if err := root1.Execute(); err != nil {
		t.Fatalf("check error: %v", err)
	}

	// Run check --json.
	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"check", "--json", "-d", destDir})
	if err := root2.Execute(); err != nil {
		t.Fatalf("check --json error: %v", err)
	}

	// Verify JSON is valid.
	decodeRecord(t, out.Bytes(), "results")

	// Verify files are unchanged.
	lockAfter, _ := os.ReadFile(lockPath)
	if string(lockBefore) != string(lockAfter) {
		t.Error("lockfile should not be modified by check")
	}

	fileAfter, _ := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
	if string(fileBefore) != string(fileAfter) {
		t.Error("skill files should not be modified by check")
	}
}

func TestCheckJSONIncludesRef(t *testing.T) {
	t.Parallel()

	bareURLWithRefs := initBareRepoWithRefs(t)
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	lf := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"alpha": {
				Source:       bareURLWithRefs,
				Ref:          "feature/install",
				SourceType:   "git",
				ComputedHash: "stale-hash",
				Dest:         destDir,
			},
		},
	}
	if err := lock.WriteFile(lock.FilePath(destDir), lf); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("check --json error: %v", err)
	}

	results := decodeRecord(t, out.Bytes(), "results")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0]["ref"] != "feature/install" {
		t.Errorf("ref = %v, want %q", results[0]["ref"], "feature/install")
	}
}

func TestCheckJSONWithoutSkills(t *testing.T) {
	t.Parallel()

	// Arrange
	destDir := filepath.Join(t.TempDir(), "skills")
	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", destDir})

	// Act
	err := root.Execute()

	// Assert
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if got := decodeRecord(t, out.Bytes(), "results"); len(got) != 0 {
		t.Errorf("results = %v, want empty", got)
	}
}

func TestCheckJSONWithFailedSkill(t *testing.T) {
	t.Parallel()

	// Arrange: the lockfile references alpha, which upstream no longer has.
	bareURL := initBareRepo(t, map[string]string{
		"skills/beta/SKILL.md": "# Beta\n",
	})
	destDir := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	lf := lock.File{Version: 1, Skills: map[string]lock.Entry{
		"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "h", Dest: destDir},
	}}
	if err := lock.WriteFile(lock.FilePath(destDir), lf); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", destDir})

	// Act
	err := root.Execute()

	// Assert
	if err == nil {
		t.Fatal("expected an error for the failed check")
	}
	var record struct {
		Results []map[string]any `json:"results"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(record.Results) != 1 || record.Error.Code != "check_failed" || !strings.Contains(record.Error.Message, "alpha") {
		t.Errorf("record = %+v, want one result and a check_failed error naming alpha", record)
	}
}
