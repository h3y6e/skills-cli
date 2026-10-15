// Package testutil builds git repository fixtures for tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Git runs git in dir, isolated from the user and system config, and
// returns its output.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}

	return string(out)
}

// WriteFiles writes files, keyed by slash-separated path, under base.
func WriteFiles(t *testing.T, base string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// InitWorkRepo creates a repository on main with a commit identity.
func InitWorkRepo(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	Git(t, work, "init", "-b", "main")
	Git(t, work, "config", "user.email", "test@test.com")
	Git(t, work, "config", "user.name", "Test")

	return work
}

// Commit writes files into work and commits everything.
func Commit(t *testing.T, work, msg string, files map[string]string) {
	t.Helper()

	WriteFiles(t, work, files)
	Git(t, work, "add", "-A")
	Git(t, work, "commit", "-m", msg)
}

// BareClone returns a file:// URL of a bare clone of work.
func BareClone(t *testing.T, work string) string {
	t.Helper()

	bare := t.TempDir()
	Git(t, work, "clone", "--bare", work, bare)

	return "file://" + bare
}

// BareRepo returns a file:// URL of a bare repo with two commits (an empty
// base, then files) so shallow clones behave realistically.
func BareRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	work := InitWorkRepo(t)
	Commit(t, work, "base", map[string]string{".gitkeep": ""})
	Commit(t, work, "add files", files)

	return BareClone(t, work)
}
