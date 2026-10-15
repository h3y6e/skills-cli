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
	"github.com/h3y6e/skolar/internal/testutil"
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

		bareURL := testutil.BareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
		})

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", skillsDir, bareURL})

		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Check.
		var out bytes.Buffer

		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "-d", skillsDir})

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

		bareURL := testutil.BareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
		})

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", skillsDir, bareURL})

		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Tamper hash.
		lockPath := lock.FilePath(skillsDir)

		lf, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}

		entry := lf.Skills["alpha"]
		entry.ComputedHash = "stale-hash"

		lf.Skills["alpha"] = entry
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer

		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "-d", skillsDir})

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

		bareURL := testutil.BareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha v2\n",
		})

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Manually install old content with stale hash.
		testutil.WriteFiles(t, skillsDir, map[string]string{"alpha/SKILL.md": "# Alpha v1\n"})

		lf := lock.File{
			Version: 1,
			Skills: map[string]lock.Entry{
				"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale"},
			},
		}
		if err := lock.WriteFile(lock.FilePath(skillsDir), lf); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer

		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"check", "-d", skillsDir})

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

	t.Run("when the directory is custom, check skips the lockfile's skills", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		dirA := filepath.Join(rootDir, ".agents", "skills")
		bareURL := testutil.BareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
			"skills/beta/SKILL.md":  "# Beta\n",
		})

		lockData := []byte(`{
		  "version": 1,
		  "skills": {
		    "alpha": {
		      "source": ` + strconv.Quote(bareURL) + `,
		      "sourceType": "git",
		      "computedHash": "stale-alpha"
		    },
		    "beta": {
		      "source": ` + strconv.Quote(bareURL) + `,
		      "sourceType": "git",
		      "computedHash": "stale-beta"
		    }
		  }
		}`)

		if err := os.MkdirAll(filepath.Dir(lock.FilePath(dirA)), 0o755); err != nil {
			t.Fatalf("mkdir lockfile parent: %v", err)
		}

		if err := os.WriteFile(lock.FilePath(dirA), lockData, 0o644); err != nil {
			t.Fatalf("write lockfile: %v", err)
		}

		var out bytes.Buffer

		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"check", "-d", filepath.Join(rootDir, "custom", "skills")})

		if err := root.Execute(); err != nil {
			t.Fatalf("check error: %v", err)
		}

		got := out.String()
		if strings.Contains(got, "alpha") || strings.Contains(got, "beta") {
			t.Errorf("did not expect skills of another directory in output for %q, got: %q", dirA, got)
		}
	})

	t.Run("reports nothing to check when lockfile is missing", func(t *testing.T) {
		t.Parallel()

		// Arrange: gh-managed destinations have no lockfile at all.
		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		var out bytes.Buffer

		root := cmd.NewRootCmd("test")
		root.SetErr(&out)
		root.SetArgs([]string{"check", "-d", skillsDir})
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

	bareURL := testutil.BareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
		"skills/beta/SKILL.md":  "# Beta\n",
	})

	t.Run("outputs valid JSON array", func(t *testing.T) {
		t.Parallel()

		skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Install.
		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", skillsDir, bareURL})

		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		// Tamper one hash.
		lockPath := lock.FilePath(skillsDir)

		lf, err := lock.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}

		entry := lf.Skills["alpha"]
		entry.ComputedHash = "stale"

		lf.Skills["alpha"] = entry
		if err := lock.WriteFile(lockPath, lf); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer

		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"check", "--json", "-d", skillsDir})

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

	bareURL := testutil.BareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha v2\n",
	})

	skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// Manually install old version with stale hash.
	testutil.WriteFiles(t, skillsDir, map[string]string{"alpha/SKILL.md": "# Alpha v1\n"})

	lf := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "stale-hash"},
		},
	}

	lockPath := lock.FilePath(skillsDir)
	if err := lock.WriteFile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	// Record lockfile content before check.
	lockBefore, _ := os.ReadFile(lockPath)
	fileBefore, _ := os.ReadFile(filepath.Join(skillsDir, "alpha", "SKILL.md"))

	// Run check (human mode).
	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"check", "-d", skillsDir})

	if err := root1.Execute(); err != nil {
		t.Fatalf("check error: %v", err)
	}

	// Run check --json.
	var out bytes.Buffer

	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"check", "--json", "-d", skillsDir})

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

	fileAfter, _ := os.ReadFile(filepath.Join(skillsDir, "alpha", "SKILL.md"))
	if string(fileBefore) != string(fileAfter) {
		t.Error("skill files should not be modified by check")
	}
}

func TestCheckJSONIncludesRef(t *testing.T) {
	t.Parallel()

	bareURLWithRefs := initBareRepoWithRefs(t)
	skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")

	lf := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"alpha": {
				Source:       bareURLWithRefs,
				Ref:          "feature/install",
				SourceType:   "git",
				ComputedHash: "stale-hash",
			},
		},
	}
	if err := lock.WriteFile(lock.FilePath(skillsDir), lf); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	var out bytes.Buffer

	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", skillsDir})

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
	skillsDir := filepath.Join(t.TempDir(), "skills")

	var out bytes.Buffer

	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", skillsDir})

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
	bareURL := testutil.BareRepo(t, map[string]string{
		"skills/beta/SKILL.md": "# Beta\n",
	})

	skillsDir := filepath.Join(t.TempDir(), ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}

	lf := lock.File{Version: 1, Skills: map[string]lock.Entry{
		"alpha": {Source: bareURL, SourceType: "git", ComputedHash: "h"},
	}}
	if err := lock.WriteFile(lock.FilePath(skillsDir), lf); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer

	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"check", "--json", "-d", skillsDir})

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
