package skill

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
)

// Status is the update state of an installed skill, or of an upstream skill
// that is not installed.
type Status string

// Statuses update reports and acts on.
const (
	StatusUpToDate        Status = "up-to-date"
	StatusUpdateAvailable Status = "update-available"
	StatusMoved           Status = "moved"
	StatusRemoved         Status = "removed"
	StatusNew             Status = "new"
	StatusCheckFailed     Status = "check-failed"
	StatusPinned          Status = "pinned"
	StatusLocal           Status = "local"
)

// UpdateCandidate represents the update status of an installed skill, or an
// upstream skill not installed yet (StatusNew).
type UpdateCandidate struct {
	SkillName   string
	Source      string
	Ref         string
	CurrentHash string
	LatestHash  string
	Status      Status
	// Upstream holds what update installs, metadata included.
	Upstream fs.FS
	// Path is the repo-relative upstream directory the skill resolved to.
	Path string
	// PreviousPath is the directory recorded at install, for a skill that
	// moved or was removed upstream.
	PreviousPath string
	// Reinstall is the add argument that installs a StatusNew skill.
	Reinstall string
	Reason    string
	// Foreign marks skills installed by gh-compatible tooling that have no
	// lockfile entry. Applying them replaces the directory only; the lockfile
	// is left untouched.
	Foreign bool
}

// Applicable reports whether update replaces the installed skill.
func (c UpdateCandidate) Applicable() bool {
	return c.Status == StatusUpdateAvailable || c.Status == StatusMoved
}

// CloneFunc returns the files of a source at its ref.
type CloneFunc func(source string) (fs.FS, error)

// SkippedEntry is a lock entry skipped due to an unsupported source type.
type SkippedEntry struct {
	SkillName  string
	SourceType lock.SourceType
}

var errRemoved = errors.New("removed upstream")

// ResolveUpdateCandidates compares the lock entries of one source against its
// upstream skills, returning update candidates sorted by name, then a
// StatusNew candidate for each upstream skill not installed. An entry
// resolves to the skill at its skillPath; when that path is gone or
// unrecorded, to the only upstream skill with its name.
func ResolveUpdateCandidates(entries map[string]lock.Entry, upstream []DiscoveredSkill) []UpdateCandidate {
	candidates := make([]UpdateCandidate, 0, len(entries))
	reinstall := ""

	for name, entry := range entries {
		reinstall = SourceInput(entry)
		candidate := UpdateCandidate{
			SkillName:   name,
			Source:      entry.Source,
			Ref:         entry.Ref,
			CurrentHash: entry.ComputedHash,
		}

		recorded := recordedDir(entry)

		up, err := resolveUpstream(name, recorded, upstream)
		switch {
		case errors.Is(err, errRemoved):
			candidate.Status, candidate.PreviousPath = StatusRemoved, recorded
		case err != nil:
			candidate.Status, candidate.Reason = StatusCheckFailed, err.Error()
		default:
			candidate.Path, candidate.Upstream = up.Path, up.FS
			if candidate.LatestHash, err = up.Hash(); err != nil {
				candidate.Status, candidate.Reason = StatusCheckFailed, err.Error()
				break
			}

			candidate.Status, candidate.PreviousPath = changeStatus(recorded, up.Path, entry.ComputedHash != candidate.LatestHash)
		}

		candidates = append(candidates, candidate)
	}

	sortCandidates(candidates)

	return append(candidates, newCandidates(candidates, upstream, reinstall)...)
}

// recordedDir returns the upstream directory an entry's skillPath records,
// or "" when it records none.
func recordedDir(entry lock.Entry) string {
	if entry.SkillPath == "" {
		return ""
	}

	return path.Dir(entry.SkillPath)
}

// changeStatus classifies a resolved skill, returning the recorded path
// when the skill moved away from it.
func changeStatus(recorded, current string, changed bool) (Status, string) {
	switch {
	case recorded != "" && recorded != current:
		return StatusMoved, recorded
	case changed:
		return StatusUpdateAvailable, ""
	default:
		return StatusUpToDate, ""
	}
}

// newCandidates returns the upstream skills installed by default that none
// of the installed candidates of the source resolved to or is named after.
func newCandidates(installed []UpdateCandidate, upstream []DiscoveredSkill, reinstall string) []UpdateCandidate {
	names, paths := map[string]bool{}, map[string]bool{}
	for _, c := range installed {
		names[c.SkillName], paths[c.Path] = true, true
	}

	var news []UpdateCandidate

	for _, s := range upstream {
		if !s.Default || names[s.Name] || paths[s.Path] {
			continue
		}

		names[s.Name] = true
		news = append(news, UpdateCandidate{
			SkillName: s.Name,
			Source:    installed[0].Source,
			Ref:       installed[0].Ref,
			Status:    StatusNew,
			Path:      s.Path,
			Reinstall: reinstall,
			Foreign:   installed[0].Foreign,
		})
	}

	return news
}

func resolveUpstream(name, recorded string, upstream []DiscoveredSkill) (DiscoveredSkill, error) {
	if i := slices.IndexFunc(upstream, func(s DiscoveredSkill) bool { return s.Path == recorded }); recorded != "" && i >= 0 {
		return upstream[i], nil
	}

	var named []DiscoveredSkill

	for _, s := range upstream {
		if s.Name == name {
			named = append(named, s)
		}
	}

	switch len(named) {
	case 0:
		return DiscoveredSkill{}, errRemoved
	case 1:
		return named[0], nil
	}

	paths := make([]string, len(named))
	for i, s := range named {
		paths[i] = s.Path
	}

	return DiscoveredSkill{}, fmt.Errorf("ambiguous upstream: %s", strings.Join(paths, ", "))
}

// FilterCandidates returns only candidates whose names filter selects. Empty filter returns all.
func FilterCandidates(candidates []UpdateCandidate, filter []string) []UpdateCandidate {
	if len(filter) == 0 {
		return candidates
	}

	var result []UpdateCandidate

	for _, c := range candidates {
		if Selected(filter, c.SkillName) {
			result = append(result, c)
		}
	}

	return result
}

// FilterDiscoveredSkills returns the skills filter selects, or the default
// skills when filter is empty. Of skills sharing a name, the first wins.
func FilterDiscoveredSkills(skills []DiscoveredSkill, filter []string) []DiscoveredSkill {
	var result []DiscoveredSkill
	for _, s := range skills {
		if slices.ContainsFunc(result, func(r DiscoveredSkill) bool { return r.Name == s.Name }) {
			continue
		}

		if len(filter) == 0 && s.Default || Selected(filter, s.Name) {
			result = append(result, s)
		}
	}

	return result
}

// CollectUpdateCandidates resolves update candidates for lockfile entries
// and gh skill installs, fetching every source in parallel. A source that
// cannot be fetched fails only its own skills. done is called after each
// source with the number fetched so far and the total. Entries of
// unsupported source types are skipped.
func CollectUpdateCandidates(
	entries map[string]lock.Entry, installed []InstalledSkill, cloneFn CloneFunc, fns UpstreamFuncs, done func(n, total int),
) ([]UpdateCandidate, []SkippedEntry) {
	supported, skipped := partitionSupportedEntries(entries)
	bySource := groupEntriesBySource(supported)
	candidates, byRepo := partitionInstalled(installed)

	var (
		mu sync.Mutex
		n  int
		wg sync.WaitGroup
	)

	finish := func(cs []UpdateCandidate) {
		mu.Lock()
		defer mu.Unlock()

		candidates = append(candidates, cs...)
		n++
		done(n, len(bySource)+len(byRepo))
	}

	for source, group := range bySource {
		wg.Go(func() { finish(resolveSourceCandidates(source, group, cloneFn)) })
	}

	for _, group := range byRepo {
		wg.Go(func() { finish(resolveRepoCandidates(group, fns)) })
	}

	wg.Wait()
	sortCandidates(candidates)

	return dropInstalledNews(candidates), skipped
}

// dropInstalledNews keeps one StatusNew candidate per source and name, and
// none for a name installed from any source: a source can hold both lockfile
// and gh skill installs, each listing the other's skills as new.
func dropInstalledNews(candidates []UpdateCandidate) []UpdateCandidate {
	installed := map[string]bool{}

	for _, c := range candidates {
		if c.Status != StatusNew {
			installed[c.SkillName] = true
		}
	}

	listed := map[[2]string]bool{}

	return slices.DeleteFunc(candidates, func(c UpdateCandidate) bool {
		key := [2]string{c.Source, c.SkillName}
		drop := c.Status == StatusNew && (installed[c.SkillName] || listed[key])
		listed[key] = listed[key] || c.Status == StatusNew

		return drop
	})
}

func resolveSourceCandidates(source string, group map[string]lock.Entry, cloneFn CloneFunc) []UpdateCandidate {
	repo, upstream, err := cloneAndDiscover(source, cloneFn)
	if err == nil {
		candidates := ResolveUpdateCandidates(group, upstream)
		for i, c := range candidates {
			if c.Status == StatusRemoved && c.PreviousPath != "" && hasSkillMD(repo, c.PreviousPath) {
				candidates[i].Status = StatusCheckFailed
				candidates[i].Reason = fmt.Sprintf("upstream %s/SKILL.md is not a valid skill", c.PreviousPath)
			}
		}

		return candidates
	}

	candidates := make([]UpdateCandidate, 0, len(group))
	for name, entry := range group {
		candidates = append(candidates, UpdateCandidate{
			SkillName:   name,
			Source:      entry.Source,
			Ref:         entry.Ref,
			CurrentHash: entry.ComputedHash,
			Status:      StatusCheckFailed,
			Reason:      err.Error(),
		})
	}

	return candidates
}

func cloneAndDiscover(source string, cloneFn CloneFunc) (fs.FS, []DiscoveredSkill, error) {
	repo, err := cloneFn(source)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch %s: %w", source, err)
	}

	upstream, err := DiscoverSkills(repo)
	if err != nil {
		return nil, nil, fmt.Errorf("discover skills in %s: %w", source, err)
	}

	return repo, upstream, nil
}

func hasSkillMD(repo fs.FS, dir string) bool {
	_, err := fs.Stat(repo, path.Join(dir, "SKILL.md"))

	return err == nil
}

// NewCloneFunc returns a CloneFunc that checks sources out through c, as
// vercel-labs/skills clones them.
func NewCloneFunc(ctx context.Context, c *git.Cache) CloneFunc {
	return func(source string) (fs.FS, error) {
		ref, err := ParseSource(source)
		if err != nil {
			return nil, err
		}

		resolved, err := git.ResolveDefault(ctx, ref.CloneURL, ref.Ref)
		if err != nil {
			return nil, err
		}

		return c.Checkout(ctx, ref.CloneURL, resolved)
	}
}

func sortCandidates(c []UpdateCandidate) {
	slices.SortFunc(c, func(a, b UpdateCandidate) int {
		return cmp.Or(cmp.Compare(a.SkillName, b.SkillName), cmp.Compare(a.Source, b.Source))
	})
}

func partitionSupportedEntries(entries map[string]lock.Entry) (map[string]lock.Entry, []SkippedEntry) {
	supported := make(map[string]lock.Entry)

	var skipped []SkippedEntry

	for name, entry := range entries {
		if entry.SourceType.Supported() {
			supported[name] = entry
			continue
		}

		skipped = append(skipped, SkippedEntry{SkillName: name, SourceType: entry.SourceType})
	}

	slices.SortFunc(skipped, func(a, b SkippedEntry) int {
		return cmp.Compare(a.SkillName, b.SkillName)
	})

	return supported, skipped
}

func groupEntriesBySource(entries map[string]lock.Entry) map[string]map[string]lock.Entry {
	bySource := make(map[string]map[string]lock.Entry)

	for name, entry := range entries {
		sourceKey := SourceInput(entry)

		group, ok := bySource[sourceKey]
		if !ok {
			group = make(map[string]lock.Entry)
			bySource[sourceKey] = group
		}

		group[name] = entry
	}

	return bySource
}
