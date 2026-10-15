package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
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

func InitWorkRepo(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	Git(t, work, "init", "-b", "main")
	Git(t, work, "config", "user.email", "test@test.com")
	Git(t, work, "config", "user.name", "Test")
	return work
}

func Commit(t *testing.T, work, msg string, files map[string]string) {
	t.Helper()

	WriteFiles(t, work, files)
	Git(t, work, "add", "-A")
	Git(t, work, "commit", "-m", msg)
}

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
