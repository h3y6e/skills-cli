package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/h3y6e/skills/cmd"
	"github.com/h3y6e/skills/internal/lock"
)

func TestListFlags(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test")
	listCmd, _, _ := root.Find([]string{"list"})

	t.Run("--json flag is registered", func(t *testing.T) {
		t.Parallel()
		flag := listCmd.Flags().Lookup("json")
		if flag == nil {
			t.Fatal("expected --json flag on list command")
		}
	})

	t.Run("filters installed skills by dest", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		destA := filepath.Join(rootDir, ".agents", "skills")
		destB := filepath.Join(rootDir, ".config", "opencode", "skills")

		lockData := []byte(`{
		  "version": 1,
		  "skills": {
		    "alpha": {
		      "source": "h3y6e/spec-skills",
		      "sourceType": "github",
		      "computedHash": "abc123",
		      "dest": ` + strconv.Quote(destA) + `
		    },
		    "beta": {
		      "source": "h3y6e/spec-skills",
		      "sourceType": "github",
		      "computedHash": "def456",
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
		root.SetArgs([]string{"list", "-d", destA})
		if err := root.Execute(); err != nil {
			t.Fatalf("list error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("expected alpha in output, got: %q", got)
		}
		if strings.Contains(got, "beta") {
			t.Errorf("did not expect beta in output for dest %q, got: %q", destA, got)
		}
	})
}

func TestListAlias(t *testing.T) {
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

	// Use ls alias.
	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"ls", "-d", destDir})
	if err := root2.Execute(); err != nil {
		t.Fatalf("ls error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "alpha") {
		t.Errorf("ls should list alpha, got: %q", got)
	}
}

func TestListHuman(t *testing.T) {
	t.Parallel()

	t.Run("lists installed skills", func(t *testing.T) {
		t.Parallel()

		bareURL := initBareRepo(t, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
			"skills/beta/SKILL.md":  "# Beta\n",
		})

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		root1 := cmd.NewRootCmd("test")
		root1.SetArgs([]string{"add", "-d", destDir, bareURL})
		if err := root1.Execute(); err != nil {
			t.Fatalf("add error: %v", err)
		}

		var out bytes.Buffer
		root2 := cmd.NewRootCmd("test")
		root2.SetOut(&out)
		root2.SetArgs([]string{"list", "-d", destDir})
		if err := root2.Execute(); err != nil {
			t.Fatalf("list error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "alpha") {
			t.Errorf("expected alpha in output, got: %q", got)
		}
		if !strings.Contains(got, "beta") {
			t.Errorf("expected beta in output, got: %q", got)
		}
	})

	t.Run("empty lockfile shows no skills message", func(t *testing.T) {
		t.Parallel()

		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		lf := lock.File{Version: 1, Skills: map[string]lock.Entry{}}
		lock.WriteFile(lock.FilePath(destDir), lf)

		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"list", "-d", destDir})
		if err := root.Execute(); err != nil {
			t.Fatalf("list error: %v", err)
		}

		got := out.String()
		if !strings.Contains(got, "no") {
			t.Errorf("expected 'no ...' message for empty lockfile, got: %q", got)
		}
	})

	t.Run("reports no installed skills when lockfile is missing", func(t *testing.T) {
		t.Parallel()

		// Arrange: gh-managed destinations have no lockfile at all.
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		var out bytes.Buffer
		root := cmd.NewRootCmd("test")
		root.SetOut(&out)
		root.SetArgs([]string{"list", "-d", destDir})
		err := root.Execute()

		// Assert
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if !strings.Contains(out.String(), "no installed skills") {
			t.Errorf("output = %q, want %q", out.String(), "no installed skills")
		}
	})
}

func TestListJSON(t *testing.T) {
	t.Parallel()

	bareURL := initBareRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "# Alpha\n",
	})

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	root1 := cmd.NewRootCmd("test")
	root1.SetArgs([]string{"add", "-d", destDir, bareURL})
	if err := root1.Execute(); err != nil {
		t.Fatalf("add error: %v", err)
	}

	var out bytes.Buffer
	root2 := cmd.NewRootCmd("test")
	root2.SetOut(&out)
	root2.SetArgs([]string{"list", "--json", "-d", destDir})
	if err := root2.Execute(); err != nil {
		t.Fatalf("list --json error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out.String())
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r["skillName"] != "alpha" {
		t.Errorf("skillName = %v, want alpha", r["skillName"])
	}

	// Must have required fields.
	for _, field := range []string{"skillName", "source", "sourceType", "computedHash"} {
		if _, ok := r[field]; !ok {
			t.Errorf("missing required JSON field %q", field)
		}
	}
}

func TestListJSONEmpty(t *testing.T) {
	t.Parallel()

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	lf := lock.File{Version: 1, Skills: map[string]lock.Entry{}}
	lock.WriteFile(lock.FilePath(destDir), lf)

	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"list", "--json", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("list --json error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out.String())
	}

	if len(results) != 0 {
		t.Errorf("expected empty array, got %d items", len(results))
	}
}

func TestListJSONIncludesRef(t *testing.T) {
	t.Parallel()

	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	lf := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"alpha": {
				Source:       "h3y6e/spec-skills",
				Ref:          "feature/install",
				SourceType:   "github",
				ComputedHash: "abc123",
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
	root.SetArgs([]string{"list", "--json", "-d", destDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("list --json error: %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, out.String())
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0]["ref"] != "feature/install" {
		t.Errorf("ref = %v, want %q", results[0]["ref"], "feature/install")
	}
}
