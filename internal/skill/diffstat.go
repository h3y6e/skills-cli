package skill

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// Stat counts the lines added and deleted, and the files changed, by
// applying an update. Mode changes do not count.
type Stat struct {
	Added   int
	Deleted int
	Files   int
}

// DiffStat counts what applying c changes in the installed skill at dir.
// Lines are counted as git diff --minimal --numstat does; binary files add
// none.
func DiffStat(c UpdateCandidate, dir string) (Stat, error) {
	want, got, err := readBoth(c.Upstream, os.DirFS(dir), !c.Foreign)
	if err != nil {
		return Stat{}, err
	}

	if c.Foreign {
		// The installed SKILL.md records the old source metadata; giving it the
		// new metadata leaves only content changes.
		meta, err := ParseSourceMetadata(string(want["SKILL.md"]))
		if err != nil {
			return Stat{}, err
		}

		if old, ok := got["SKILL.md"]; ok {
			injected, err := InjectSourceMetadata(string(old), meta)
			if err != nil {
				return Stat{}, err
			}

			got["SKILL.md"] = []byte(injected)
		}
	}

	var stat Stat

	for _, p := range changedFiles(want, got) {
		added, deleted := lineDiff(got[p], want[p])
		stat.Added += added
		stat.Deleted += deleted
		stat.Files++
	}

	// Update replaces the whole directory, so links, which copies leave out,
	// go too.
	links, err := countLinks(dir)
	stat.Files += links

	return stat, err
}

func countLinks(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type()&fs.ModeSymlink != 0 {
			n++
		}

		return err
	})

	return n, err
}

// lineDiff counts the lines a minimal line diff from a to b adds and
// deletes, or none when either is binary, as git judges by a NUL byte.
func lineDiff(a, b []byte) (added, deleted int) {
	if isBinary(a) || isBinary(b) {
		return 0, 0
	}

	from, to := splitLines(a), splitLines(b)
	common := lcsLength(internLines(from, to))

	return len(to) - common, len(from) - common
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

func splitLines(data []byte) []string {
	var lines []string

	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n') + 1
		if i == 0 {
			i = len(data)
		}

		lines = append(lines, string(data[:i]))
		data = data[i:]
	}

	return lines
}

// internLines numbers the lines of a and b, dropping those found on one side
// only, which no common subsequence can contain.
func internLines(a, b []string) (x, y []int) {
	ids := map[string]int{}
	sides := map[string]int{}

	for _, l := range a {
		sides[l] |= 1
	}

	for _, l := range b {
		sides[l] |= 2
	}

	number := func(lines []string) []int {
		var out []int

		for _, l := range lines {
			if sides[l] != 3 {
				continue
			}

			id, ok := ids[l]
			if !ok {
				id = len(ids)
				ids[l] = id
			}

			out = append(out, id)
		}

		return out
	}

	return number(a), number(b)
}

// lcsLength returns the length of a longest common subsequence of x and y,
// with Myers' O((N+M)D) algorithm.
func lcsLength(x, y []int) int {
	n, m := len(x), len(y)
	offset := n + m + 1
	v := make([]int, 2*offset+1)

	for d := 0; d <= n+m; d++ {
		for k := -d; k <= d; k += 2 {
			var i int
			if k == -d || k != d && v[offset+k-1] < v[offset+k+1] {
				i = v[offset+k+1]
			} else {
				i = v[offset+k-1] + 1
			}

			j := i - k
			for i < n && j < m && x[i] == y[j] {
				i++
				j++
			}

			v[offset+k] = i

			if i >= n && j >= m {
				return (n + m - d) / 2
			}
		}
	}

	return 0
}
