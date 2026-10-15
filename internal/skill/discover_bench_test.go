package skill_test

import (
	"testing"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/h3y6e/skolar/internal/testutil"
)

func BenchmarkDiscoverSkills(b *testing.B) {
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

		s, err := c.Fetch(b.Context(), repo, ref)
		if err != nil {
			b.Fatal(err)
		}

		if _, err := skill.DiscoverSkills(s.FS); err != nil {
			b.Fatal(err)
		}

		_ = c.Close()
	}
}
