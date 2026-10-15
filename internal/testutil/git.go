// Package testutil builds git repository fixtures for tests.
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Main runs a package's tests with git isolated from the user's config and
// skolar's cache from the user's.
func Main(m *testing.M) {
	home, err := os.MkdirTemp("", "skolar-test-home-*")
	if err != nil {
		panic(err)
	}

	for name, value := range map[string]string{
		"HOME":              home,
		"XDG_CACHE_HOME":    filepath.Join(home, ".cache"),
		"GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_CONFIG_SYSTEM": os.DevNull,
	} {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}

	code := m.Run()

	_ = os.RemoveAll(home)

	os.Exit(code)
}

// Git runs git in dir, isolated from the user and system config, and
// returns its output.
func Git(tb testing.TB, dir string, args ...string) string {
	tb.Helper()

	cmd := exec.CommandContext(tb.Context(), "git", args...)
	cmd.Dir = dir

	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("git %v failed: %v\n%s", args, err, out)
	}

	return string(out)
}

// WriteFiles writes files, keyed by slash-separated path, under base.
func WriteFiles(tb testing.TB, base string, files map[string]string) {
	tb.Helper()

	for name, content := range files {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tb.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

// InitWorkRepo creates a repository on main with a commit identity.
func InitWorkRepo(tb testing.TB) string {
	tb.Helper()

	work := tb.TempDir()
	Git(tb, work, "init", "-b", "main")
	Git(tb, work, "config", "user.email", "test@test.com")
	Git(tb, work, "config", "user.name", "Test")

	return work
}

// Commit writes files into work and commits everything.
func Commit(tb testing.TB, work, msg string, files map[string]string) {
	tb.Helper()

	WriteFiles(tb, work, files)
	Git(tb, work, "add", "-A")
	Git(tb, work, "commit", "-m", msg)
}

// BareClone returns a file:// URL of a bare clone of work.
func BareClone(tb testing.TB, work string) string {
	tb.Helper()

	bare := tb.TempDir()
	Git(tb, work, "clone", "--bare", work, bare)

	return "file://" + bare
}

// BareRepo returns a file:// URL of a bare repo with two commits (an empty
// base, then files) so shallow clones behave realistically.
func BareRepo(tb testing.TB, files map[string]string) string {
	tb.Helper()

	work := InitWorkRepo(tb)
	Commit(tb, work, "base", map[string]string{".gitkeep": ""})
	Commit(tb, work, "add files", files)

	return BareClone(tb, work)
}

// SkillRepo returns a file:// URL of a bare repo shaped like a large skill
// collection: skills under skills/<group>/<name>, each with a SKILL.md and
// files reference files.
func SkillRepo(tb testing.TB, skills, files int) string {
	tb.Helper()

	work := InitWorkRepo(tb)
	body := strings.Repeat("Explain the step, then show an example of it.\n", 80)

	all := map[string]string{}

	for i := range skills {
		dir := fmt.Sprintf("skills/group-%d/skill-%d", i%8, i)
		all[dir+"/SKILL.md"] = fmt.Sprintf("---\nname: skill-%d\ndescription: Skill %d.\n---\n%s", i, i, body)

		for j := range files {
			all[fmt.Sprintf("%s/references/ref-%d.md", dir, j)] = fmt.Sprintf("# Reference %d\n%s", j, body)
		}
	}

	Commit(tb, work, "skills", all)

	return BareClone(tb, work)
}
