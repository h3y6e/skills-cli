//go:build interop

// Package interop runs skolar against the upstream CLIs pinned by flake.lock
// (vercel-labs/skills as `skills`, and `gh skill`) on the public fixture repo.
package interop

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	fixtureRepo = "h3y6e/skolar-fixtures"
	fixtureURL  = "https://github.com/" + fixtureRepo
)

var skolar string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "skolar-interop-*")
	if err != nil {
		panic(err)
	}
	// The sandboxed HOME hides gh's stored login, so pass it as GH_TOKEN.
	if os.Getenv("GH_TOKEN") == "" {
		if token, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output(); err == nil {
			if err := os.Setenv("GH_TOKEN", strings.TrimSpace(string(token))); err != nil {
				panic(err)
			}
		}
	}

	skolar = filepath.Join(dir, "skolar")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", skolar, "github.com/h3y6e/skolar")

	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}

	code := m.Run()

	os.RemoveAll(dir)
	os.Exit(code)
}

var homes sync.Map

// env gives each scenario's subprocesses a HOME of their own, so gh skill's
// global lock and other user state stay out of the real home directory.
func env(t *testing.T) []string {
	t.Helper()
	home, _ := homes.LoadOrStore(t.Name(), t.TempDir())
	h, _ := home.(string)

	return append(os.Environ(),
		"HOME="+h, "XDG_CONFIG_HOME="+filepath.Join(h, ".config"), "XDG_STATE_HOME="+filepath.Join(h, ".local", "state"),
		"DO_NOT_TRACK=1", "DISABLE_TELEMETRY=1", "NO_COLOR=1")
}

func runStderr(t *testing.T, dir, name string, args ...string) (string, string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = env(t)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", name, strings.Join(args, " "), err, out, stderr.String())
	}

	return string(out), stderr.String()
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, _ := runStderr(t, dir, name, args...)

	return out
}

// ghUpdate runs gh skill update and fails on the warnings gh prints when it
// skips a skill, since it still exits 0.
func ghUpdate(t *testing.T, dir string, args ...string) {
	t.Helper()

	_, stderr := runStderr(t, filepath.Dir(dir), "gh", append([]string{"skill", "update", "--dir", dir}, args...)...)
	for line := range strings.Lines(stderr) {
		if strings.HasPrefix(line, "!") {
			t.Errorf("gh skill update warned: %s", strings.TrimSpace(line))
		}
	}
}

func vercelAdd(t *testing.T, project string) {
	t.Helper()
	run(t, project, "skills", "add", fixtureURL, "--skill", "*", "--agent", "universal", "--copy", "--full-depth", "-y")
}

func ghInstall(t *testing.T, dir string, args ...string) {
	t.Helper()
	run(t, filepath.Dir(dir), "gh", append([]string{"skill", "install", fixtureRepo, "--dir", dir}, args...)...)
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(b)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return files
}

func skillNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}

	return names
}

func assertSameFiles(t *testing.T, want, got map[string]string) {
	t.Helper()

	for _, k := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[k]; !ok {
			t.Errorf("missing %s", k)
		} else if g != want[k] {
			t.Errorf("%s differs\nwant:\n%s\ngot:\n%s", k, want[k], g)
		}
	}

	for _, k := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected %s", k)
		}
	}
}

func subtree(files map[string]string, prefix string) map[string]string {
	out := map[string]string{}

	for k, v := range files {
		if rest, ok := strings.CutPrefix(k, prefix+"/"); ok {
			out[rest] = v
		}
	}

	return out
}

type updateResult struct {
	SkillName    string `json:"skillName"`
	Status       string `json:"status"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
	Reason       string `json:"reason"`
}

// planUpdate returns what skolar update would do to the installed skills in dest.
func planUpdate(t *testing.T, dest string) []updateResult {
	t.Helper()

	var record struct {
		Results []updateResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(run(t, filepath.Dir(filepath.Dir(dest)), skolar, "update", "--dry-run", "-d", dest, "--json")), &record); err != nil {
		t.Fatal(err)
	}

	installed := slices.DeleteFunc(record.Results, func(r updateResult) bool { return r.Status == "new" })

	var planned []string
	for _, r := range installed {
		planned = append(planned, r.SkillName)
	}

	if names := skillNames(t, dest); !slices.Equal(slices.Sorted(slices.Values(planned)), names) {
		t.Fatalf("skolar update covered %v, want every installed skill %v", planned, names)
	}

	return installed
}

func assertAllUpToDate(t *testing.T, dest string) {
	t.Helper()

	for _, r := range planUpdate(t, dest) {
		if r.Status != "up-to-date" {
			t.Errorf("%s: %s %s", r.SkillName, r.Status, r.Reason)
		}
	}
}

func assertMoved(t *testing.T, dest, name, from, to string) {
	t.Helper()

	for _, r := range planUpdate(t, dest) {
		switch {
		case r.SkillName == name && (r.Status != "moved" || r.PreviousPath != from || r.Path != to):
			t.Errorf("%s: %s from %q to %q, want moved from %q to %q", name, r.Status, r.PreviousPath, r.Path, from, to)
		case r.SkillName != name && r.Status != "up-to-date":
			t.Errorf("%s: %s %s", r.SkillName, r.Status, r.Reason)
		}
	}
}

func TestVercelFormat(t *testing.T) {
	t.Run("when vercel installed the skills, skolar reports them up to date and update changes nothing", func(t *testing.T) {
		// Arrange
		project := t.TempDir()
		vercelAdd(t, project)
		before := snapshot(t, project)

		// Act
		assertAllUpToDate(t, filepath.Join(project, ".agents", "skills"))
		run(t, project, skolar, "update", "-y")

		// Assert
		assertSameFiles(t, before, snapshot(t, project))
	})

	t.Run("when the lock entries have no skillPath, skolar still finds every skill vercel installed", func(t *testing.T) {
		// Arrange
		project := t.TempDir()
		vercelAdd(t, project)
		editLock(t, project, func(entries map[string]map[string]any) {
			for _, e := range entries {
				delete(e, "skillPath")
			}
		})

		// Act & Assert
		assertAllUpToDate(t, filepath.Join(project, ".agents", "skills"))
	})

	t.Run("when a skillPath no longer exists upstream, skolar reports the move and update restores the lock entry vercel writes", func(t *testing.T) {
		// Arrange
		project := t.TempDir()
		vercelAdd(t, project)
		before := snapshot(t, project)
		editLock(t, project, func(entries map[string]map[string]any) {
			entries["alpha"]["skillPath"] = "moved/alpha/SKILL.md"
		})
		dest := filepath.Join(project, ".agents", "skills")

		// Act
		assertMoved(t, dest, "alpha", "moved/alpha", "skills/alpha")
		run(t, project, skolar, "update", "-y")

		// Assert
		assertSameFiles(t, before, snapshot(t, project))
	})

	t.Run("when skolar installs every skill vercel installs into a lockfile project, files and lock entries match and vercel update changes nothing", func(t *testing.T) {
		// Arrange
		vercel, own := t.TempDir(), t.TempDir()
		vercelAdd(t, vercel)

		if err := os.WriteFile(filepath.Join(own, "skills-lock.json"), []byte(`{"version":1,"skills":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}

		args := []string{"add", fixtureURL, "-y"}
		for _, name := range skillNames(t, filepath.Join(vercel, ".agents", "skills")) {
			args = append(args, "-s", name)
		}

		// Act
		run(t, own, skolar, args...)
		installed := snapshot(t, own)
		run(t, own, "skills", "update", "-p", "-y")

		// Assert
		want := snapshot(t, vercel)
		for _, name := range skillNames(t, filepath.Join(vercel, ".agents", "skills")) {
			dir := ".agents/skills/" + name
			assertSameFiles(t, subtree(want, dir), subtree(installed, dir))
		}

		var wantLock, gotLock struct {
			Skills map[string]json.RawMessage `json:"skills"`
		}
		if err := json.Unmarshal([]byte(want["skills-lock.json"]), &wantLock); err != nil {
			t.Fatal(err)
		}

		if err := json.Unmarshal([]byte(installed["skills-lock.json"]), &gotLock); err != nil {
			t.Fatal(err)
		}

		for name, entry := range wantLock.Skills {
			got, ok := gotLock.Skills[name]
			if !ok {
				t.Errorf("lock entry %s missing", name)
				continue
			}

			if !bytes.Equal(compactJSON(t, entry), compactJSON(t, got)) {
				t.Errorf("lock entry %s\nwant: %s\ngot:  %s", name, entry, got)
			}
		}

		assertSameFiles(t, installed, snapshot(t, own))
	})
}

func editLock(t *testing.T, project string, edit func(entries map[string]map[string]any)) {
	t.Helper()

	path := filepath.Join(project, "skills-lock.json")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var lf struct {
		Version int                       `json:"version"`
		Skills  map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(b, &lf); err != nil {
		t.Fatal(err)
	}

	edit(lf.Skills)

	b, err = json.MarshalIndent(lf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()

	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}

	return b.Bytes()
}

func TestGhFormat(t *testing.T) {
	t.Run("when gh installed the skills, skolar reports them up to date and update changes nothing", func(t *testing.T) {
		// Arrange
		dest := filepath.Join(t.TempDir(), "skills")
		ghInstall(t, dest, "--all")
		before := snapshot(t, dest)

		// Act
		assertAllUpToDate(t, dest)
		run(t, filepath.Dir(dest), skolar, "update", "-d", dest, "-y")

		// Assert
		assertSameFiles(t, before, snapshot(t, dest))
	})

	t.Run("when a github-path no longer exists upstream, skolar reports the move and update restores the files gh installs", func(t *testing.T) {
		// Arrange
		dest := filepath.Join(t.TempDir(), "skills")
		ghInstall(t, dest, "alpha")
		before := snapshot(t, dest)
		skillMD := filepath.Join(dest, "alpha", "SKILL.md")

		if err := os.WriteFile(skillMD, []byte(strings.Replace(before["alpha/SKILL.md"], "github-path: skills/alpha", "github-path: moved/alpha", 1)), 0o644); err != nil {
			t.Fatal(err)
		}

		// Act
		assertMoved(t, dest, "alpha", "moved/alpha", "skills/alpha")
		run(t, filepath.Dir(dest), skolar, "update", "-d", dest, "-y")

		// Assert
		assertSameFiles(t, before, snapshot(t, dest))
	})

	t.Run("when skolar installs without a lockfile, files match a gh install and gh update changes nothing", func(t *testing.T) {
		// Arrange
		gh, own := filepath.Join(t.TempDir(), "skills"), filepath.Join(t.TempDir(), "skills")
		ghInstall(t, gh, "--all")

		// Act
		run(t, filepath.Dir(own), skolar, "add", fixtureRepo, "-d", own, "-y")
		installed := snapshot(t, own)
		ghUpdate(t, own, "--all")

		// Assert
		want := snapshot(t, gh)
		for _, name := range skillNames(t, gh) {
			assertSameFiles(t, subtree(want, name), subtree(installed, name))
		}

		assertSameFiles(t, installed, snapshot(t, own))
	})

	t.Run("when an unpinned install is behind the latest release, skolar update yields the same files as gh update", func(t *testing.T) {
		// Arrange
		gh, own := filepath.Join(t.TempDir(), "skills"), filepath.Join(t.TempDir(), "skills")
		for _, dest := range []string{gh, own} {
			ghInstall(t, dest, "alpha", "--pin", "v1.0.0")
			unpin(t, filepath.Join(dest, "alpha", "SKILL.md"))
		}

		// Act
		ghUpdate(t, gh, "--all")
		run(t, filepath.Dir(own), skolar, "update", "-d", own, "-y")

		// Assert
		assertSameFiles(t, snapshot(t, gh), snapshot(t, own))
	})

	t.Run("when skolar updated a gh install and upstream is unchanged, gh update changes nothing", func(t *testing.T) {
		// Arrange
		dest := filepath.Join(t.TempDir(), "skills")
		ghInstall(t, dest, "alpha", "--pin", "v1.0.0")
		unpin(t, filepath.Join(dest, "alpha", "SKILL.md"))
		run(t, filepath.Dir(dest), skolar, "update", "-d", dest, "-y")
		before := snapshot(t, dest)

		// Act
		ghUpdate(t, dest, "--all")

		// Assert
		assertSameFiles(t, before, snapshot(t, dest))
	})

	t.Run("when gh reinstalls a skill skolar updated, the files match a fresh gh install", func(t *testing.T) {
		// Arrange
		gh, own := filepath.Join(t.TempDir(), "skills"), filepath.Join(t.TempDir(), "skills")
		ghInstall(t, gh, "alpha")
		ghInstall(t, own, "alpha", "--pin", "v1.0.0")
		unpin(t, filepath.Join(own, "alpha", "SKILL.md"))
		run(t, filepath.Dir(own), skolar, "update", "-d", own, "-y")

		// Act
		ghUpdate(t, own, "--all", "--force")

		// Assert
		assertSameFiles(t, snapshot(t, gh), snapshot(t, own))
	})
}

// unpin turns a pinned gh install into an unpinned one at the same version.
func unpin(t *testing.T, skillMD string) {
	t.Helper()

	b, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatal(err)
	}

	var kept []string

	for line := range strings.Lines(string(b)) {
		if !strings.Contains(line, "github-pinned:") {
			kept = append(kept, line)
		}
	}

	if err := os.WriteFile(skillMD, []byte(strings.Join(kept, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGhUndiscoverableLayouts(t *testing.T) {
	for _, tc := range []struct{ skill, path string }{
		{"root-skill", "."},
		{"theta", "extras/theta"},
	} {
		t.Run("when skolar records "+tc.skill+" in gh format, the metadata holds its real path and gh update leaves the outdated install alone until gh can discover "+tc.path, func(t *testing.T) {
			// Arrange
			dir := filepath.Join(t.TempDir(), "skills")
			run(t, filepath.Dir(dir), skolar, "add", fixtureRepo+"#v1.0.0", "-s", tc.skill, "-d", dir, "-y")
			skillMD := filepath.Join(dir, tc.skill, "SKILL.md")
			unpin(t, skillMD)
			before := snapshot(t, dir)

			// Act
			ghUpdate(t, dir, "--all")

			// Assert
			if got := metadataValue(before[tc.skill+"/SKILL.md"], "github-path"); got != tc.path {
				t.Errorf("github-path = %q, want %q", got, tc.path)
			}

			assertSameFiles(t, before, snapshot(t, dir))
		})
	}
}

func metadataValue(skillMD, key string) string {
	for line := range strings.Lines(skillMD) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), key+":"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}

	return ""
}

func TestRecordChoice(t *testing.T) {
	t.Run("when the project has a lockfile, skolar records a skill added to a custom directory in gh metadata and leaves the lockfile alone", func(t *testing.T) {
		// Arrange
		project := t.TempDir()
		vercelAdd(t, project)
		lockBefore := snapshot(t, project)["skills-lock.json"]
		dir := filepath.Join(project, "dot_agents", "exact_skills")

		// Act
		run(t, project, skolar, "add", fixtureRepo, "-s", "alpha", "-d", dir, "-y")

		// Assert
		if got := metadataValue(snapshot(t, dir)["alpha/SKILL.md"], "github-repo"); got != fixtureURL {
			t.Errorf("github-repo = %q, want %q", got, fixtureURL)
		}

		if got := snapshot(t, project)["skills-lock.json"]; got != lockBefore {
			t.Errorf("skills-lock.json changed:\n%s", got)
		}
	})

	t.Run("when the directory is custom, adding a source gh metadata can't record fails and writes nothing", func(t *testing.T) {
		// Arrange
		src := t.TempDir()
		run(t, src, "git", "init", "-q")

		if err := os.MkdirAll(filepath.Join(src, "skills", "local"), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(src, "skills", "local", "SKILL.md"), []byte("---\nname: local\ndescription: d\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		run(t, src, "git", "add", "-A")
		run(t, src, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
		project := t.TempDir()
		dir := filepath.Join(project, "custom", "skills")

		// Act
		cmd := exec.CommandContext(t.Context(), skolar, "add", "file://"+src, "-d", dir, "-y")
		cmd.Dir = project
		out, err := cmd.CombinedOutput()

		// Assert
		if err == nil {
			t.Fatalf("skolar add succeeded:\n%s", out)
		}

		if files := snapshot(t, project); len(files) != 0 {
			t.Errorf("skolar add wrote files: %v", slices.Sorted(maps.Keys(files)))
		}
	})
}
