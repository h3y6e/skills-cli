package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	// Reinstall is the add argument that restores the skill as recorded.
	Reinstall string `json:"-"`
}

// VerifyLocked recomputes each lockfile skill's computedHash from disk,
// offline. When the hash differs, it clones the source: if the upstream skill
// still has the recorded hash, the install is compared file by file with it,
// minus what vercel-labs/skills leaves out when it installs a skill.
func VerifyLocked(layout lock.Layout, entries map[string]lock.Entry, cloneFn CloneFunc) []VerifyResult {
	clones := map[string]string{}
	results := make([]VerifyResult, 0, len(entries))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[name]
		result := VerifyResult{SkillName: name, Source: entry.Source, Status: VerifyVerified, Reinstall: SourceInput(entry)}
		hash, err := ComputeHash(layout.SkillDir(name))
		switch {
		case err != nil:
			result.Status, result.Reason = VerifyUnverifiable, err.Error()
		case hash != entry.ComputedHash:
			result.Status, result.Files, result.Reason = verifyLockedOnline(name, entry, layout.SkillDir(name), cloneFn, clones)
		}
		results = append(results, result)
	}
	return results
}

func verifyLockedOnline(name string, entry lock.Entry, installed string, cloneFn CloneFunc, clones map[string]string) (VerifyStatus, []string, string) {
	source := SourceInput(entry)
	cloneDir, ok := clones[source]
	if !ok {
		var err error
		if cloneDir, err = cloneFn(source); err != nil {
			return VerifyUnverifiable, nil, fmt.Sprintf("clone %s: %v", source, err)
		}
		clones[source] = cloneDir
	}
	upstream, err := DiscoverSkills(cloneDir)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	up, err := resolveUpstream(name, entry, upstream)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if up.ComputedHash != entry.ComputedHash {
		return VerifyUnverifiable, nil, "upstream changed since install"
	}
	files, err := diffDirs(up.Dir, installed, true)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if len(files) > 0 {
		return VerifyModified, files, ""
	}
	return VerifyVerified, nil, ""
}

// VerifyTracked compares each gh-format skill with the upstream tree its
// metadata records, fetched at github-ref.
func VerifyTracked(installed []InstalledSkill, fns UpstreamFuncs) []VerifyResult {
	clones := map[[2]string]string{}
	results := make([]VerifyResult, 0, len(installed))
	for _, s := range installed {
		source, _ := OwnerRepoFromURL(s.Meta.RepoURL)
		result := VerifyResult{SkillName: s.Name, Source: source, Reinstall: FormatSourceInput(source, s.Meta.Pinned)}
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
	files, err := diffDirs(staged, s.Dir, false)
	if err != nil {
		return VerifyUnverifiable, nil, err.Error()
	}
	if len(files) > 0 {
		return VerifyModified, files, ""
	}
	return VerifyVerified, nil, ""
}

// VerifyUnrecorded reports every skill directory in skillsDir that neither the
// lockfile nor readable gh skill metadata records.
func VerifyUnrecorded(skillsDir string, recorded func(name string) bool) ([]VerifyResult, error) {
	entries, err := os.ReadDir(skillsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills directory %q: %w", skillsDir, err)
	}
	var results []VerifyResult
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || recorded(e.Name()) {
			continue
		}
		reason := "no lockfile entry or gh skill metadata"
		if data, err := os.ReadFile(filepath.Join(skillsDir, e.Name(), "SKILL.md")); err != nil {
			reason = err.Error()
		} else if _, err := ParseSourceMetadata(string(data)); err != nil {
			reason = err.Error()
		}
		results = append(results, VerifyResult{SkillName: e.Name(), Status: VerifyUnverifiable, Reason: reason})
	}
	return results, nil
}

// diffDirs returns the sorted paths of regular files that differ, or exist
// in only one of want and got. With vercel, want is read as vercel-labs/skills
// installs it.
func diffDirs(want, got string, vercel bool) ([]string, error) {
	wantFiles, err := readFiles(want, vercel)
	if err != nil {
		return nil, err
	}
	gotFiles, err := readFiles(got, false)
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

func readFiles(dir string, vercel bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipCopy(d, vercel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
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
