package cmd_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h3y6e/skills/cmd"
	"github.com/h3y6e/skills/internal/git"
	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
)

// initBareRepoWithTwoTags creates a bare repo where skills/alpha has v1.0.0
// (older) and v2.0.0 (latest) tags, and skills/beta exists at both.
func initBareRepoWithTwoTags(t *testing.T) string {
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
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "test@test.com")
	gitRun("config", "user.name", "Test")

	write("skills/alpha/SKILL.md", "# Alpha v1\n")
	write("skills/beta/SKILL.md", "# Beta v1\n")
	gitRun("add", "-A")
	gitRun("commit", "-m", "v1")
	gitRun("tag", "v1.0.0")

	write("skills/alpha/SKILL.md", "# Alpha v2\n")
	write("skills/beta/SKILL.md", "# Beta v2\n")
	gitRun("add", "-A")
	gitRun("commit", "-m", "v2")
	gitRun("tag", "v2.0.0")

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

// stubGitHubTransport redirects git operations for the given GitHub
// "owner/repo" shorthand to a local bare repository, so GitHub-mode flows
// can be exercised end to end without network access. Not parallel-safe.
func stubGitHubTransport(t *testing.T, canonical, bareURL string) {
	t.Helper()

	cloneURL := "https://github.com/" + canonical + ".git"
	repoURL := "https://github.com/" + canonical

	origClone, origResolve, origUpstream := cmd.ShallowClone, cmd.ResolveRef, cmd.NewUpstreamFuncs

	cmd.ShallowClone = func(ctx context.Context, url, ref, dest string) error {
		if url == cloneURL {
			url = bareURL
		}
		return origClone(ctx, url, ref, dest)
	}
	cmd.ResolveRef = func(ctx context.Context, url, ref string) (git.ResolvedRef, error) {
		if url == cloneURL {
			url = bareURL
		}
		return origResolve(ctx, url, ref)
	}
	cmd.NewUpstreamFuncs = func(ctx context.Context, prefix string) (skill.UpstreamFuncs, func()) {
		real, cleanup := origUpstream(ctx, prefix)
		return skill.UpstreamFuncs{
			FetchLatest: func(url string) (string, git.ResolvedRef, error) {
				if url == repoURL {
					url = bareURL
				}
				return real.FetchLatest(url)
			},
			TreeSHA: real.TreeSHA,
		}, cleanup
	}

	t.Cleanup(func() {
		cmd.ShallowClone = origClone
		cmd.ResolveRef = origResolve
		cmd.NewUpstreamFuncs = origUpstream
	})
}

func runSkills(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := cmd.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func readMetadata(t *testing.T, destDir, name string) skill.SourceMetadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(destDir, name, "SKILL.md"))
	if err != nil {
		t.Fatalf("expected %s/SKILL.md: %v", name, err)
	}
	meta, err := skill.ParseSourceMetadata(string(data))
	if err != nil {
		t.Fatalf("ParseSourceMetadata() error = %v", err)
	}
	return meta
}

func TestAddTrackedMode(t *testing.T) {
	// Not parallel: stubs the git transport.

	t.Run("when no lockfile exists, add installs in gh skill format without creating one", func(t *testing.T) {
		// Arrange
		stubGitHubTransport(t, "h3y6e/spec-skills", initBareRepoWithTwoTags(t))
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		output, err := runSkills(t, "add", "-s", "alpha", "-d", destDir, "h3y6e/spec-skills")

		// Assert
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		if !strings.Contains(output, "installed alpha") {
			t.Errorf("output = %q, want it to contain %q", output, "installed alpha")
		}
		meta := readMetadata(t, destDir, "alpha")
		if meta.RepoURL != "https://github.com/h3y6e/spec-skills" {
			t.Errorf("RepoURL = %q", meta.RepoURL)
		}
		if meta.Ref != "refs/tags/v2.0.0" {
			t.Errorf("Ref = %q, want %q (latest release)", meta.Ref, "refs/tags/v2.0.0")
		}
		if meta.TreeSHA == "" || meta.Path != "skills/alpha" || meta.Pinned != "" {
			t.Errorf("unexpected metadata: %+v", meta)
		}
		if _, err := os.Stat(lock.FilePath(destDir)); !os.IsNotExist(err) {
			t.Error("no lockfile should be created in gh skill format")
		}
	})

	t.Run("when a lockfile exists, add uses lock mode without injecting metadata", func(t *testing.T) {
		// Arrange
		bareURL := initBareRepoWithTwoTags(t)
		stubGitHubTransport(t, "h3y6e/spec-skills", bareURL)
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")
		seed := lock.File{Version: 1, Skills: map[string]lock.Entry{}}
		if err := lock.WriteFile(lock.FilePath(destDir), seed); err != nil {
			t.Fatalf("seed lockfile: %v", err)
		}

		// Act
		_, err := runSkills(t, "add", "-s", "alpha", "-d", destDir, bareURL)

		// Assert
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		lf, err := lock.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if _, ok := lf.Skills["alpha"]; !ok {
			t.Error("lockfile should contain alpha in lock mode")
		}
		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if string(data) != "# Alpha v2\n" {
			t.Errorf("lock mode must not inject metadata, content = %q", string(data))
		}
	})

	t.Run("when the source uses a ref fragment, the install is pinned to that version", func(t *testing.T) {
		// Arrange
		stubGitHubTransport(t, "h3y6e/spec-skills", initBareRepoWithTwoTags(t))
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		_, err := runSkills(t, "add", "-s", "alpha", "-d", destDir, "h3y6e/spec-skills#v1.0.0")

		// Assert
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		meta := readMetadata(t, destDir, "alpha")
		if meta.Pinned != "v1.0.0" {
			t.Errorf("Pinned = %q, want %q", meta.Pinned, "v1.0.0")
		}
		if meta.Ref != "refs/tags/v1.0.0" {
			t.Errorf("Ref = %q, want %q", meta.Ref, "refs/tags/v1.0.0")
		}
		data, err := os.ReadFile(filepath.Join(destDir, "alpha", "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md: %v", err)
		}
		if !strings.Contains(string(data), "# Alpha v1") {
			t.Errorf("content = %q, want Alpha v1", string(data))
		}
	})

	t.Run("when the source is local and no lockfile exists, add keeps lock mode", func(t *testing.T) {
		// Arrange
		bareURL := initBareRepo(t, map[string]string{
			"skills/foo/SKILL.md": "# Foo\n",
		})
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		_, err := runSkills(t, "add", "-d", destDir, bareURL)

		// Assert
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		lf, err := lock.ReadFile(lock.FilePath(destDir))
		if err != nil {
			t.Fatalf("read lockfile: %v", err)
		}
		if _, ok := lf.Skills["foo"]; !ok {
			t.Error("local source should use lock mode even without a lockfile")
		}
	})

	t.Run("when the source is not github or local and no lockfile exists, add falls back to lock mode", func(t *testing.T) {
		// Arrange
		destDir := filepath.Join(t.TempDir(), ".agents", "skills")

		// Act
		_, err := runSkills(t, "add", "-d", destDir, "https://gitlab.com/example/repo.git")

		// Assert
		if err == nil {
			t.Fatal("add should fail at clone in lock mode")
		}
		if strings.Contains(err.Error(), "gh skill format") {
			t.Errorf("gitlab source must not enter gh skill format, error = %v", err)
		}
	})
}

func TestE2ETrackedLifecycle(t *testing.T) {
	// Not parallel: overrides IsTTY and stubs the git transport.
	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func() bool { return false }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	stubGitHubTransport(t, "h3y6e/spec-skills", initBareRepoWithTwoTags(t))
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	// --- add (gh skill format) ---
	if _, err := runSkills(t, "add", "-s", "alpha", "-d", destDir, "h3y6e/spec-skills"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// --- list shows the tracked skill without a lockfile ---
	listOut, err := runSkills(t, "list", "-d", destDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "alpha") {
		t.Fatalf("list should show alpha, got: %q", listOut)
	}

	// --- check reports up-to-date ---
	checkOut, err := runSkills(t, "check", "-d", destDir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "alpha: up-to-date") {
		t.Fatalf("check should report alpha up-to-date, got: %q", checkOut)
	}

	// --- tamper with the install to simulate an outdated skill ---
	alphaPath := filepath.Join(destDir, "alpha", "SKILL.md")
	meta := readMetadata(t, destDir, "alpha")
	meta.TreeSHA = "stale-tree-sha"
	data, _ := os.ReadFile(alphaPath)
	tampered, err := skill.InjectSourceMetadata(string(data), meta)
	if err != nil {
		t.Fatalf("InjectSourceMetadata() error = %v", err)
	}
	tampered = strings.Replace(tampered, "# Alpha v2", "# Alpha outdated", 1)
	if err := os.WriteFile(alphaPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- check reports update available ---
	checkOut, err = runSkills(t, "check", "-d", destDir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "alpha: update available") {
		t.Fatalf("check should report update available, got: %q", checkOut)
	}

	// --- update re-downloads and re-injects metadata ---
	if _, err := runSkills(t, "update", "-y", "-d", destDir); err != nil {
		t.Fatalf("update: %v", err)
	}
	data, _ = os.ReadFile(alphaPath)
	if !strings.Contains(string(data), "# Alpha v2") {
		t.Errorf("update should restore upstream content, got:\n%s", data)
	}
	meta = readMetadata(t, destDir, "alpha")
	if meta.TreeSHA == "stale-tree-sha" || meta.TreeSHA == "" {
		t.Errorf("update should refresh github-tree-sha, got %q", meta.TreeSHA)
	}
	if _, err := os.Stat(lock.FilePath(destDir)); !os.IsNotExist(err) {
		t.Error("update must not create a lockfile for tracked skills")
	}

	// --- check is up-to-date again ---
	checkOut, err = runSkills(t, "check", "-d", destDir)
	if err != nil {
		t.Fatalf("check after update: %v", err)
	}
	if !strings.Contains(checkOut, "alpha: up-to-date") {
		t.Fatalf("check after update should be up-to-date, got: %q", checkOut)
	}

	// --- remove deletes the skill without a lockfile ---
	if _, err := runSkills(t, "remove", "-d", destDir, "alpha"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "alpha")); !os.IsNotExist(err) {
		t.Error("alpha directory should not exist after remove")
	}
	if _, err := os.Stat(lock.FilePath(destDir)); !os.IsNotExist(err) {
		t.Error("remove must not create a lockfile for tracked skills")
	}
}

func TestE2ETrackedPinnedIsSkipped(t *testing.T) {
	// Not parallel: overrides IsTTY and stubs the git transport.
	origIsTTY := cmd.IsTTY
	cmd.IsTTY = func() bool { return false }
	t.Cleanup(func() { cmd.IsTTY = origIsTTY })

	stubGitHubTransport(t, "h3y6e/spec-skills", initBareRepoWithTwoTags(t))
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")

	if _, err := runSkills(t, "add", "-s", "alpha", "-d", destDir, "h3y6e/spec-skills#v1.0.0"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// --- check reports pinned ---
	checkOut, err := runSkills(t, "check", "-d", destDir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "alpha: pinned") {
		t.Fatalf("check should report alpha pinned, got: %q", checkOut)
	}

	// --- update skips pinned ---
	updateOut, err := runSkills(t, "update", "-y", "-d", destDir)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(updateOut, "alpha: pinned to v1.0.0 (skipped)") {
		t.Fatalf("update should skip pinned alpha, got: %q", updateOut)
	}
	meta := readMetadata(t, destDir, "alpha")
	if meta.Pinned != "v1.0.0" || meta.Ref != "refs/tags/v1.0.0" {
		t.Errorf("pinned metadata should be untouched, got %+v", meta)
	}
}

func TestListMixedLockfileAndTrackedSkills(t *testing.T) {
	t.Parallel()

	// Arrange: a lock-managed entry and a foreign tracked skill.
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	seed := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"beta": {Source: "h3y6e/spec-skills", SourceType: "github", ComputedHash: "abc123", Dest: destDir},
		},
	}
	if err := lock.WriteFile(lock.FilePath(destDir), seed); err != nil {
		t.Fatalf("seed lockfile: %v", err)
	}
	content, err := skill.InjectSourceMetadata("# Alpha\n", skill.SourceMetadata{
		RepoURL: "https://github.com/h3y6e/agent-skills",
		Ref:     "refs/tags/v2026.7.1",
		TreeSHA: "7a5507a41b20be505102f26e6b75927edd1493fd",
		Path:    "skills/alpha",
	})
	if err != nil {
		t.Fatalf("InjectSourceMetadata() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(destDir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "alpha", "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	listOut, err := runSkills(t, "list", "-d", destDir)

	// Assert
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, "alpha") || !strings.Contains(listOut, "beta") {
		t.Fatalf("list should show both alpha and beta, got: %q", listOut)
	}
}
