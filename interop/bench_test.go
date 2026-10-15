//go:build interop

package interop

import (
	"os"
	"path/filepath"
	"testing"
)

type contender struct {
	tool string
	act  func(b *testing.B, home, dir string)
}

func compare(b *testing.B, format string, prepare func(b *testing.B) string, contenders ...contender) {
	b.Helper()

	for _, cache := range []string{"cold", "warm"} {
		for _, c := range contenders {
			b.Run("format="+format+"/cache="+cache+"/tool="+c.tool, func(b *testing.B) {
				var dir string
				if prepare != nil {
					dir = prepare(b)
				}

				home := homeOf(b)
				if cache == "warm" {
					c.act(b, home, dir)
				}

				for b.Loop() {
					if cache == "cold" {
						home = b.TempDir()
					}

					c.act(b, home, dir)
				}
			})
		}
	}
}

func BenchmarkInstall(b *testing.B) {
	vercel := b.TempDir()
	vercelAdd(b, vercel)

	vercelArgs := []string{"add", fixtureURL, "-y"}
	for _, name := range skillNames(b, filepath.Join(vercel, ".agents", "skills")) {
		vercelArgs = append(vercelArgs, "-s", name)
	}

	compare(b, "vercel", nil,
		contender{"skolar", func(b *testing.B, home, _ string) {
			b.Helper()

			project := b.TempDir()
			if err := os.WriteFile(filepath.Join(project, "skills-lock.json"), []byte(`{"version":1,"skills":{}}`), 0o644); err != nil {
				b.Fatal(err)
			}

			runIn(b, home, project, skolar, vercelArgs...)
		}},
		contender{"skills", func(b *testing.B, home, _ string) {
			b.Helper()

			runIn(b, home, b.TempDir(), "skills", "add", fixtureURL, "--skill", "*", "--agent", "universal", "--copy", "--full-depth", "-y")
		}},
	)

	gh := filepath.Join(b.TempDir(), "skills")
	ghInstall(b, gh, "--all")

	ghArgs := []string{"add", fixtureRepo, "-y"}
	for _, name := range skillNames(b, gh) {
		ghArgs = append(ghArgs, "-s", name)
	}

	compare(b, "gh", nil,
		contender{"skolar", func(b *testing.B, home, _ string) {
			b.Helper()

			parent := b.TempDir()
			runIn(b, home, parent, skolar, append(ghArgs, "-d", filepath.Join(parent, "skills"))...)
		}},
		contender{"gh", func(b *testing.B, home, _ string) {
			b.Helper()

			parent := b.TempDir()
			runIn(b, home, parent, "gh", "skill", "install", fixtureRepo, "--dir", filepath.Join(parent, "skills"), "--all")
		}},
	)
}

func BenchmarkUpdate(b *testing.B) {
	compare(b, "vercel",
		func(b *testing.B) string {
			b.Helper()

			project := b.TempDir()
			vercelAdd(b, project)

			return project
		},
		contender{"skolar", func(b *testing.B, home, dir string) {
			b.Helper()

			runIn(b, home, dir, skolar, "update", "-y")
		}},
		contender{"skills", func(b *testing.B, home, dir string) {
			b.Helper()

			runIn(b, home, dir, "skills", "update", "-p", "-y")
		}},
	)

	compare(b, "gh",
		func(b *testing.B) string {
			b.Helper()

			dest := filepath.Join(b.TempDir(), "skills")
			ghInstall(b, dest, "--all")

			return dest
		},
		contender{"skolar", func(b *testing.B, home, dir string) {
			b.Helper()

			runIn(b, home, filepath.Dir(dir), skolar, "update", "-d", dir, "-y")
		}},
		contender{"gh", func(b *testing.B, home, dir string) {
			b.Helper()

			ghUpdate(b, home, dir, "--all")
		}},
	)
}
