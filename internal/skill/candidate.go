package skill

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/h3y6e/skills/internal/git"
	"github.com/h3y6e/skills/internal/lock"
)

type Status string

const (
	StatusUpToDate        Status = "up-to-date"
	StatusUpdateAvailable Status = "update-available"
	StatusCheckFailed     Status = "check-failed"
	StatusPinned          Status = "pinned"
	StatusLocal           Status = "local"
)

func (s Status) String() string { return string(s) }

// DiscoveredSkill represents a skill found in an upstream repository.
type DiscoveredSkill struct {
	Name         string
	Dir          string
	Path         string // repo-relative directory of the skill, e.g. "skills/foo"
	ComputedHash string
}

// UpdateCandidate represents the update status of an installed skill.
type UpdateCandidate struct {
	SkillName   string
	Source      string
	Ref         string
	CurrentHash string
	LatestHash  string
	Status      Status
	StagedDir   string
	Reason      string
	// Foreign marks skills installed by gh-compatible tooling that have no
	// lockfile entry. Applying them replaces the directory only; the lockfile
	// is left untouched.
	Foreign bool
}

// CloneFunc clones a source and returns the directory path. Caller must clean up.
type CloneFunc func(source string) (string, error)

// SkippedEntry is a lock entry skipped due to an unsupported source type.
type SkippedEntry struct {
	SkillName  string
	SourceType string
}

// ResolveUpdateCandidates compares lock entries against upstream skills
// and returns update candidates sorted by name.
func ResolveUpdateCandidates(entries map[string]lock.Entry, upstream []DiscoveredSkill) []UpdateCandidate {
	upstreamByName := make(map[string]DiscoveredSkill, len(upstream))
	for _, s := range upstream {
		upstreamByName[s.Name] = s
	}

	candidates := make([]UpdateCandidate, 0, len(entries))
	for name, entry := range entries {
		up, found := upstreamByName[name]
		if !found {
			candidates = append(candidates, UpdateCandidate{
				SkillName:   name,
				Source:      entry.Source,
				Ref:         entry.Ref,
				CurrentHash: entry.ComputedHash,
				Status:      StatusCheckFailed,
				Reason:      fmt.Sprintf("skill %q not found in upstream source", name),
			})
			continue
		}

		status := StatusUpToDate
		if entry.ComputedHash != up.ComputedHash {
			status = StatusUpdateAvailable
		}

		candidates = append(candidates, UpdateCandidate{
			SkillName:   name,
			Source:      entry.Source,
			Ref:         entry.Ref,
			CurrentHash: entry.ComputedHash,
			LatestHash:  up.ComputedHash,
			Status:      status,
			StagedDir:   up.Dir,
		})
	}

	sortCandidates(candidates)
	return candidates
}

// FilterCandidates returns only candidates whose names are in filter. Empty filter returns all.
func FilterCandidates(candidates []UpdateCandidate, filter []string) []UpdateCandidate {
	if len(filter) == 0 {
		return candidates
	}
	var result []UpdateCandidate
	for _, c := range candidates {
		if slices.Contains(filter, c.SkillName) {
			result = append(result, c)
		}
	}
	return result
}

// FilterDiscoveredSkills returns only skills whose names are in filter. Empty filter returns all.
func FilterDiscoveredSkills(skills []DiscoveredSkill, filter []string) []DiscoveredSkill {
	if len(filter) == 0 {
		return skills
	}
	var result []DiscoveredSkill
	for _, s := range skills {
		if slices.Contains(filter, s.Name) {
			result = append(result, s)
		}
	}
	return result
}

// AggregateUpdateCandidates groups lock entries by source, clones each, discovers
// upstream skills, and returns aggregated update candidates. Unsupported entries are skipped.
func AggregateUpdateCandidates(entries map[string]lock.Entry, cloneFn CloneFunc) ([]UpdateCandidate, []SkippedEntry, error) {
	if len(entries) == 0 {
		return nil, nil, nil
	}

	supported, skipped := partitionSupportedEntries(entries)
	if len(supported) == 0 {
		return nil, skipped, nil
	}

	bySource := groupEntriesBySource(supported)
	results := make(chan aggregateResult, len(bySource))
	var wg sync.WaitGroup

	for source, group := range bySource {
		source, group := source, group
		wg.Add(1)
		go func() {
			defer wg.Done()

			cloneDir, err := cloneFn(source)
			if err != nil {
				results <- aggregateResult{err: fmt.Errorf("clone source %q: %w", source, err)}
				return
			}

			upstream, err := DiscoverSkills(cloneDir)
			if err != nil {
				results <- aggregateResult{err: fmt.Errorf("discover skills in %q: %w", source, err)}
				return
			}

			results <- aggregateResult{candidates: ResolveUpdateCandidates(group, upstream)}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var all []UpdateCandidate
	var errs []error
	for result := range results {
		if result.err != nil {
			errs = append(errs, result.err)
			continue
		}
		all = append(all, result.candidates...)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, skipped, err
	}

	sortCandidates(all)
	return all, skipped, nil
}

// NewCloneFunc returns a CloneFunc that shallow-clones into temp directories,
// and a cleanup function that removes all created temp directories.
func NewCloneFunc(ctx context.Context, prefix string) (CloneFunc, func()) {
	var dirs []string
	var mu sync.Mutex

	cloneFn := func(source string) (string, error) {
		ref, err := ParseSource(source)
		if err != nil {
			return "", err
		}
		tmpDir, err := os.MkdirTemp("", prefix)
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
		if err := git.ShallowClone(ctx, ref.CloneURL, ref.Ref, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			return "", err
		}
		mu.Lock()
		dirs = append(dirs, tmpDir)
		mu.Unlock()
		return tmpDir, nil
	}

	cleanup := func() {
		mu.Lock()
		cleanupDirs := append([]string(nil), dirs...)
		mu.Unlock()

		for _, d := range cleanupDirs {
			os.RemoveAll(d)
		}
	}

	return cloneFn, cleanup
}

type aggregateResult struct {
	candidates []UpdateCandidate
	err        error
}

func sortCandidates(c []UpdateCandidate) {
	slices.SortFunc(c, func(a, b UpdateCandidate) int {
		return cmp.Compare(a.SkillName, b.SkillName)
	})
}

func partitionSupportedEntries(entries map[string]lock.Entry) (map[string]lock.Entry, []SkippedEntry) {
	supported := make(map[string]lock.Entry)
	var skipped []SkippedEntry

	for name, entry := range entries {
		if SupportedSourceType(entry.SourceType) {
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
		sourceKey := FormatSourceInput(entry.Source, entry.Ref)
		group, ok := bySource[sourceKey]
		if !ok {
			group = make(map[string]lock.Entry)
			bySource[sourceKey] = group
		}
		group[name] = entry
	}
	return bySource
}
