package skill

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
)

// InstalledSkill is a skill present in a destination directory whose SKILL.md
// carries gh-compatible source metadata in its frontmatter.
type InstalledSkill struct {
	Name string
	Dir  string
	Meta SourceMetadata
}

// ScanInstalledSkills returns the skills in skillsDir that carry gh-compatible
// source metadata, sorted by name. Skills without metadata, with unreadable
// SKILL.md, or with invalid frontmatter are skipped.
func ScanInstalledSkills(skillsDir string) ([]InstalledSkill, error) {
	entries, err := os.ReadDir(skillsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read skills directory %q: %w", skillsDir, err)
	}

	var installed []InstalledSkill

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(skillsDir, entry.Name())

		data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			continue
		}

		meta, err := ParseSourceMetadata(string(data))
		if err != nil || (!meta.IsGitHub() && !meta.IsLocal()) {
			continue
		}

		installed = append(installed, InstalledSkill{
			Name: entry.Name(),
			Dir:  dir,
			Meta: meta,
		})
	}

	slices.SortFunc(installed, func(a, b InstalledSkill) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return installed, nil
}

// PreferTrackedInstall reports whether add should install in gh skill format
// (frontmatter metadata, no lockfile): when no lockfile exists yet and the
// source is a GitHub repository. Other source types keep the lockfile format,
// which unlike gh skill metadata can track them for updates.
func PreferTrackedInstall(lockfileExists bool, sourceType lock.SourceType) bool {
	return !lockfileExists && sourceType == lock.SourceGitHub
}

// UnmanagedSkills returns installed skills whose names are absent from the
// lockfile-managed entries.
func UnmanagedSkills(installed []InstalledSkill, managed map[string]lock.Entry) []InstalledSkill {
	var foreign []InstalledSkill

	for _, s := range installed {
		if _, ok := managed[s.Name]; !ok {
			foreign = append(foreign, s)
		}
	}

	return foreign
}

// FilterInstalledBySource narrows installed skills to those installed from
// the given source repository and, when the source has a ref, pinned to it.
// An empty source keeps all skills. Skills installed from a local path never
// match a repository source.
func FilterInstalledBySource(installed []InstalledSkill, source string) ([]InstalledSkill, error) {
	if source == "" {
		return installed, nil
	}

	ref, err := ParseSource(source)
	if err != nil {
		return nil, err
	}

	var filtered []InstalledSkill

	for _, s := range installed {
		if !s.Meta.IsGitHub() {
			continue
		}

		ownerRepo, err := OwnerRepoFromURL(s.Meta.RepoURL)
		if err != nil || ownerRepo != ref.CanonicalSource || s.Meta.Pinned != ref.Ref {
			continue
		}

		filtered = append(filtered, s)
	}

	return filtered, nil
}

// UpstreamFuncs groups the git operations used to probe upstream
// repositories. Replaceable in tests.
type UpstreamFuncs struct {
	// FetchLatest fetches the latest ref of repoURL (gh skill semantics:
	// latest release, then default branch).
	FetchLatest func(repoURL string) (git.Snapshot, error)
	// FetchRef fetches repoURL at ref.
	FetchRef func(repoURL, ref string) (git.Snapshot, error)
}

// NewUpstreamFuncs returns the production UpstreamFuncs along with a cleanup
// function releasing what they fetched.
func NewUpstreamFuncs(ctx context.Context) (UpstreamFuncs, func()) {
	var snaps snapshots

	fns := UpstreamFuncs{
		FetchLatest: func(repoURL string) (git.Snapshot, error) {
			resolved, err := git.ResolveLatest(ctx, repoURL, "")
			if err != nil {
				return git.Snapshot{}, err
			}

			return snaps.add(git.Fetch(ctx, repoURL, resolved))
		},
		FetchRef: func(repoURL, ref string) (git.Snapshot, error) {
			resolved, err := git.ResolveDefault(ctx, repoURL, ref)
			if err != nil {
				return git.Snapshot{}, err
			}

			return snaps.add(git.Fetch(ctx, repoURL, resolved))
		},
	}

	return fns, snaps.close
}

// partitionInstalled reports pinned, local and unlocatable gh skill installs
// as candidates without fetching, and groups the rest by repository. Update
// detection compares github-tree-sha with the upstream tree at the latest
// ref, as gh skill update does.
func partitionInstalled(installed []InstalledSkill) ([]UpdateCandidate, map[string][]InstalledSkill) {
	byRepo := make(map[string][]InstalledSkill)

	var candidates []UpdateCandidate

	for _, s := range installed {
		switch {
		case s.Meta.Pinned != "":
			candidates = append(candidates, pinnedCandidate(s))
		case s.Meta.IsLocal():
			candidates = append(candidates, UpdateCandidate{
				SkillName: s.Name,
				Status:    StatusLocal,
				Reason:    s.Meta.LocalPath,
				Foreign:   true,
			})
		case s.Meta.Path == "":
			candidates = append(candidates, failedCandidate(s, "missing github-path in skill metadata"))
		default:
			byRepo[s.Meta.RepoURL] = append(byRepo[s.Meta.RepoURL], s)
		}
	}

	return candidates, byRepo
}

func trackedCandidate(s InstalledSkill) UpdateCandidate {
	source, _ := OwnerRepoFromURL(s.Meta.RepoURL)

	return UpdateCandidate{SkillName: s.Name, Source: source, CurrentHash: s.Meta.TreeSHA, Foreign: true}
}

func failedCandidate(s InstalledSkill, reason string) UpdateCandidate {
	c := trackedCandidate(s)
	c.Status, c.Reason = StatusCheckFailed, reason

	return c
}

func resolveRepoCandidates(group []InstalledSkill, fns UpstreamFuncs) []UpdateCandidate {
	repoURL := group[0].Meta.RepoURL

	snap, upstream, err := fetchRepo(repoURL, fns)
	if err != nil {
		candidates := make([]UpdateCandidate, 0, len(group))
		for _, s := range group {
			candidates = append(candidates, failedCandidate(s, err.Error()))
		}

		return candidates
	}

	candidates := make([]UpdateCandidate, 0, len(group))
	for _, s := range group {
		candidate := trackedCandidate(s)
		candidate.Ref = snap.Ref.Full

		up, err := resolveTrackedUpstream(s, snap.FS, upstream)
		switch {
		case errors.Is(err, errRemoved):
			candidate.Status, candidate.PreviousPath = StatusRemoved, s.Meta.Path
		case err != nil:
			candidate.Status, candidate.Reason = StatusCheckFailed, err.Error()
		default:
			candidate = resolveTracked(candidate, s, up, snap)
		}

		candidates = append(candidates, candidate)
	}

	sortCandidates(candidates)

	return append(candidates, newCandidates(candidates, upstream, candidates[0].Source)...)
}

// resolveTrackedUpstream finds the upstream skill at github-path even when
// discovery skips it or it is not a directory, so that it fails on its own
// content rather than passing for removed.
func resolveTrackedUpstream(s InstalledSkill, repo fs.FS, upstream []DiscoveredSkill) (DiscoveredSkill, error) {
	if mode, ok := lstat(repo, s.Meta.Path); ok && fs.ValidPath(s.Meta.Path) && (!mode.IsDir() || hasSkillMD(repo, s.Meta.Path)) {
		return DiscoveredSkill{Name: s.Name, Path: s.Meta.Path, FS: subFS(repo, s.Meta.Path)}, nil
	}

	return resolveUpstream(s.Name, s.Meta.Path, upstream)
}

func fetchRepo(repoURL string, fns UpstreamFuncs) (git.Snapshot, []DiscoveredSkill, error) {
	source, err := OwnerRepoFromURL(repoURL)
	if err != nil {
		return git.Snapshot{}, nil, err
	}

	snap, err := fns.FetchLatest(repoURL)
	if err != nil {
		return git.Snapshot{}, nil, fmt.Errorf("fetch %s: %w", source, err)
	}

	upstream, err := DiscoverSkills(snap.FS)
	if err != nil {
		return git.Snapshot{}, nil, fmt.Errorf("discover skills in %s: %w", source, err)
	}

	return snap, upstream, nil
}

// resolveTracked compares an installed skill with the upstream skill it
// resolved to, staging the upstream skill with fresh metadata when update
// would replace it.
func resolveTracked(candidate UpdateCandidate, s InstalledSkill, up DiscoveredSkill, snap git.Snapshot) UpdateCandidate {
	treeSHA, err := snap.Tree(up.Path)
	if err != nil {
		candidate.Status, candidate.Reason = StatusCheckFailed, err.Error()
		return candidate
	}

	candidate.Path, candidate.LatestHash = up.Path, treeSHA
	candidate.Status, candidate.PreviousPath = changeStatus(s.Meta.Path, up.Path, treeSHA != s.Meta.TreeSHA)

	if !candidate.Applicable() {
		return candidate
	}

	candidate.Upstream, err = withMetadata(up.FS, SourceMetadata{
		RepoURL: s.Meta.RepoURL,
		Ref:     snap.Ref.Full,
		TreeSHA: treeSHA,
		Path:    up.Path,
	})
	if err != nil {
		candidate.Status, candidate.Reason = StatusCheckFailed, err.Error()
	}

	return candidate
}

func pinnedCandidate(s InstalledSkill) UpdateCandidate {
	source, _ := OwnerRepoFromURL(s.Meta.RepoURL)

	return UpdateCandidate{
		SkillName: s.Name,
		Source:    source,
		Ref:       s.Meta.Ref,
		Status:    StatusPinned,
		Reason:    s.Meta.Pinned,
		Foreign:   true,
	}
}
