package skill

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/h3y6e/skolar/internal/lock"
)

type VerifyStatus string

const (
	VerifyVerified     VerifyStatus = "verified"
	VerifyModified     VerifyStatus = "modified"
	VerifyUnverifiable VerifyStatus = "unverifiable"
	VerifyLocal        VerifyStatus = "local"
)

// VerifyResult compares an installed skill with its record. Files lists the
// differing paths when the record allows a per-file comparison.
type VerifyResult struct {
	SkillName string       `json:"skillName"`
	Status    VerifyStatus `json:"status"`
	Source    string       `json:"source"`
	Files     []string     `json:"files,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

// VerifyLocked recomputes each lockfile skill's computedHash from disk.
func VerifyLocked(layout lock.Layout, entries map[string]lock.Entry) []VerifyResult {
	results := make([]VerifyResult, 0, len(entries))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[name]
		result := VerifyResult{SkillName: name, Source: entry.Source, Status: VerifyVerified}
		hash, err := ComputeHash(layout.SkillDir(name))
		switch {
		case err != nil:
			result.Status, result.Reason = VerifyUnverifiable, err.Error()
		case hash != entry.ComputedHash:
			result.Status = VerifyModified
		}
		results = append(results, result)
	}
	return results
}

// VerifyTracked compares each gh-format skill with the upstream tree its
// metadata records, fetched at github-ref.
func VerifyTracked(installed []InstalledSkill, fns UpstreamFuncs) []VerifyResult {
	clones := map[[2]string]string{}
	results := make([]VerifyResult, 0, len(installed))
	for _, s := range installed {
		source, _ := OwnerRepoFromURL(s.Meta.RepoURL)
		result := VerifyResult{SkillName: s.Name, Source: source}
		switch {
		case s.Meta.IsLocal():
			result.Status, result.Reason = VerifyLocal, s.Meta.LocalPath
		case s.Meta.Path == "":
			result.Status, result.Reason = VerifyUnverifiable, "missing github-path in skill metadata"
		default:
			result.Status, result.Files, result.Reason = verifyTracked(s, fns, clones)
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b VerifyResult) int { return cmp.Compare(a.SkillName, b.SkillName) })
	return results
}

func verifyTracked(s InstalledSkill, fns UpstreamFuncs, clones map[[2]string]string) (VerifyStatus, []string, string) {
	key := [2]string{s.Meta.RepoURL, s.Meta.Ref}
	cloneDir, ok := clones[key]
	if !ok {
		var err error
		cloneDir, err = fns.FetchRef(s.Meta.RepoURL, s.Meta.Ref)
		if err != nil {
			return VerifyUnverifiable, nil, fmt.Sprintf("fetch %s: %v", s.Meta.Ref, err)
		}
		clones[key] = cloneDir
	}

	treeSHA, err := fns.TreeSHA(cloneDir, s.Meta.Path)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if treeSHA != s.Meta.TreeSHA {
		return VerifyUnverifiable, nil, fmt.Sprintf("%s no longer holds tree %s", s.Meta.Ref, s.Meta.TreeSHA)
	}

	staged, err := copyToStaging(cloneDir, filepath.Join(cloneDir, filepath.FromSlash(s.Meta.Path)))
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if err := injectStagedMetadata(staged, s.Meta); err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	files, err := diffDirs(staged, s.Dir)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if len(files) > 0 {
		return VerifyModified, files, ""
	}
	return VerifyVerified, nil, ""
}

// diffDirs returns the sorted paths of regular files that differ, or exist
// in only one of want and got.
func diffDirs(want, got string) ([]string, error) {
	wantFiles, err := readFiles(want)
	if err != nil {
		return nil, err
	}
	gotFiles, err := readFiles(got)
	if err != nil {
		return nil, err
	}
	var diff []string
	for path, data := range wantFiles {
		if other, ok := gotFiles[path]; !ok || !bytes.Equal(data, other) {
			diff = append(diff, path)
		}
	}
	for path := range gotFiles {
		if _, ok := wantFiles[path]; !ok {
			diff = append(diff, path)
		}
	}
	slices.Sort(diff)
	return diff, nil
}

func readFiles(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}
