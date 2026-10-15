package skill

import (
	"fmt"
	"path"
	"slices"
)

// Selected reports whether name matches one of patterns, shell globs such
// as "gke-*" that path.Match takes.
func Selected(patterns []string, name string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool {
		ok, _ := path.Match(p, name)
		return ok
	})
}

// CheckPatterns reports the first malformed pattern.
func CheckPatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("skill pattern %q: %w", p, err)
		}
	}

	return nil
}
