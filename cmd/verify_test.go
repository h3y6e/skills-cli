package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/cmd"
)

type verifyRecord struct {
	Results []struct {
		SkillName string   `json:"skillName"`
		Status    string   `json:"status"`
		Files     []string `json:"files"`
		Reason    string   `json:"reason"`
	} `json:"results"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func runVerifyJSON(t *testing.T, dir string) (verifyRecord, error) {
	t.Helper()
	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetArgs([]string{"verify", "--json", "-d", dir})
	err := root.Execute()
	var rec verifyRecord
	if jsonErr := json.Unmarshal(out.Bytes(), &rec); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\n%s", jsonErr, out.String())
	}
	return rec, err
}

func addSkills(t *testing.T, args ...string) {
	t.Helper()
	root := cmd.NewRootCmd("test")
	root.SetArgs(append([]string{"add", "-y"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("add %v: %v", args, err)
	}
}

func TestVerifyLockfile(t *testing.T) {
	t.Parallel()
	bareURL := initBareRepo(t, map[string]string{"skills/alpha/SKILL.md": "# Alpha\n"})

	t.Run("when a lockfile skill is untouched, verify reports it verified", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := filepath.Join(t.TempDir(), ".agents", "skills")
		addSkills(t, "-d", dir, bareURL)

		// Act
		rec, err := runVerifyJSON(t, dir)

		// Assert
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rec.Results) != 1 || rec.Results[0].Status != "verified" {
			t.Errorf("results = %+v, want alpha verified", rec.Results)
		}
	})

	t.Run("when a lockfile skill's file changed, verify reports it modified and fails with verify_failed", func(t *testing.T) {
		t.Parallel()
		// Arrange
		dir := filepath.Join(t.TempDir(), ".agents", "skills")
		addSkills(t, "-d", dir, bareURL)
		if err := os.WriteFile(filepath.Join(dir, "alpha", "SKILL.md"), []byte("# Tampered\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Act
		rec, err := runVerifyJSON(t, dir)

		// Assert
		if err == nil || rec.Error.Code != "verify_failed" {
			t.Fatalf("verify error = %v, code %q, want verify_failed", err, rec.Error.Code)
		}
		if len(rec.Results) != 1 || rec.Results[0].Status != "modified" {
			t.Errorf("results = %+v, want alpha modified", rec.Results)
		}
	})
}

func TestVerifyGhMetadata(t *testing.T) {
	// Not parallel: stubs the GitHub transport.
	bareURL := initBareRepoWithTwoTags(t)
	stubGitHubTransport(t, "h3y6e/fixture", bareURL)

	t.Run("when a gh-format skill is untouched, verify reports it verified", func(t *testing.T) {
		// Arrange
		dir := filepath.Join(t.TempDir(), "skills")
		addSkills(t, "-s", "alpha", "-d", dir, "h3y6e/fixture")

		// Act
		rec, err := runVerifyJSON(t, dir)

		// Assert
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if len(rec.Results) != 1 || rec.Results[0].Status != "verified" {
			t.Errorf("results = %+v, want alpha verified", rec.Results)
		}
	})

	t.Run("when a gh-format skill gained and changed files, verify lists them as modified", func(t *testing.T) {
		// Arrange
		dir := filepath.Join(t.TempDir(), "skills")
		addSkills(t, "-s", "alpha", "-d", dir, "h3y6e/fixture")
		skillMD := filepath.Join(dir, "alpha", "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skillMD, []byte(strings.Replace(string(data), "Alpha v2", "Alpha v9", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "alpha", "extra.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		// Act
		rec, err := runVerifyJSON(t, dir)

		// Assert
		if err == nil {
			t.Fatal("verify should fail")
		}
		if len(rec.Results) != 1 || rec.Results[0].Status != "modified" || !slices.Equal(rec.Results[0].Files, []string{"SKILL.md", "extra.md"}) {
			t.Errorf("results = %+v, want alpha modified in SKILL.md and extra.md", rec.Results)
		}
	})

	t.Run("when the recorded tree no longer matches its ref, verify reports the skill unverifiable", func(t *testing.T) {
		// Arrange
		dir := filepath.Join(t.TempDir(), "skills")
		addSkills(t, "-s", "alpha", "-d", dir, "h3y6e/fixture")
		skillMD := filepath.Join(dir, "alpha", "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			t.Fatal(err)
		}
		tree := metadataLine(t, string(data), "github-tree-sha")
		if err := os.WriteFile(skillMD, []byte(strings.Replace(string(data), tree, "0000000000000000000000000000000000000000", 1)), 0o644); err != nil {
			t.Fatal(err)
		}

		// Act
		rec, err := runVerifyJSON(t, dir)

		// Assert
		if err == nil {
			t.Fatal("verify should fail")
		}
		if len(rec.Results) != 1 || rec.Results[0].Status != "unverifiable" {
			t.Errorf("results = %+v, want alpha unverifiable", rec.Results)
		}
	})
}

func metadataLine(t *testing.T, skillMD, key string) string {
	t.Helper()
	for line := range strings.Lines(skillMD) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("no %s in:\n%s", key, skillMD)
	return ""
}
