package git_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/testutil"
)

func BenchmarkFetch(b *testing.B) {
	repo := testutil.SkillRepo(b, 170, 4)

	ref, err := git.ResolveDefault(b.Context(), repo, "")
	if err != nil {
		b.Fatal(err)
	}

	warm := git.NewCache()
	if _, err := warm.Fetch(b.Context(), repo, ref); err != nil {
		b.Fatal(err)
	}

	_ = warm.Close()

	for b.Loop() {
		c := git.NewCache()
		if _, err := c.Fetch(b.Context(), repo, ref); err != nil {
			b.Fatal(err)
		}

		_ = c.Close()
	}
}

func BenchmarkReadTree(b *testing.B) {
	repo := testutil.SkillRepo(b, 170, 4)

	ref, err := git.ResolveDefault(b.Context(), repo, "")
	if err != nil {
		b.Fatal(err)
	}

	setup := git.NewCache()

	s, err := setup.Fetch(b.Context(), repo, ref)
	if err != nil {
		b.Fatal(err)
	}

	var trees []string

	err = fs.WalkDir(s.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && strings.HasPrefix(d.Name(), "skill-") {
			tree, err := s.TreeSHA(p)
			trees = append(trees, tree)

			return err
		}

		return err
	})
	if err != nil {
		b.Fatal(err)
	}

	_ = setup.Close()

	for b.Loop() {
		c := git.NewCache()

		for _, tree := range trees {
			t, err := c.ReadTree(b.Context(), repo, tree)
			if err != nil {
				b.Fatal(err)
			}

			err = fs.WalkDir(t, ".", func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					_, err = fs.ReadFile(t, p)
				}

				return err
			})
			if err != nil {
				b.Fatal(err)
			}
		}

		_ = c.Close()
	}
}
