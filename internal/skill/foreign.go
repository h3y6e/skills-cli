package skill

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/h3y6e/skills/internal/git"
	"github.com/h3y6e/skills/internal/lock"
)

// InstalledSkill is a skill present in a destination directory whose SKILL.md
// carries gh-compatible source metadata in its frontmatter.
type InstalledSkill struct {
	Name string
	Dir  string
	Meta SourceMetadata
}

// ScanInstalledSkills returns the skills in destDir that carry gh-compatible
// source metadata, sorted by name. Skills without metadata, with unreadable
// SKILL.md, or with invalid frontmatter are skipped.
func ScanInstalledSkills(destDir string) ([]InstalledSkill, error) {
	entries, err := os.ReadDir(destDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills directory %q: %w", destDir, err)
	}

	var installed []InstalledSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(destDir, entry.Name())
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
func PreferTrackedInstall(lockfileExists bool, sourceType string) bool {
	return !lockfileExists && sourceType == "github"
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
	// FetchLatest resolves the latest ref of repoURL (gh skill semantics:
	// latest release, then default branch) and clones it into a new temp
	// directory, returning the clone path and the resolved ref.
	FetchLatest func(repoURL string) (cloneDir string, ref git.ResolvedRef, err error)
	// TreeSHA returns the git tree SHA of path within a repository directory.
	TreeSHA func(repoDir, path string) (string, error)
}

// NewUpstreamFuncs returns the production UpstreamFuncs along with a cleanup
// function removing all cloned temp directories.
func NewUpstreamFuncs(ctx context.Context, tmpPrefix string) (UpstreamFuncs, func()) {
	var dirs []string

	fns := UpstreamFuncs{
		FetchLatest: func(repoURL string) (string, git.ResolvedRef, error) {
			resolved, err := git.ResolveRef(ctx, repoURL, "")
			if err != nil {
				return "", git.ResolvedRef{}, err
			}
			tmpDir, err := os.MkdirTemp("", tmpPrefix)
			if err != nil {
				return "", git.ResolvedRef{}, fmt.Errorf("create temp dir: %w", err)
			}
			if err := git.ShallowClone(ctx, repoURL, resolved.Full, tmpDir); err != nil {
				os.RemoveAll(tmpDir)
				return "", git.ResolvedRef{}, err
			}
			dirs = append(dirs, tmpDir)
			return tmpDir, resolved, nil
		},
		TreeSHA: func(repoDir, path string) (string, error) {
			return git.TreeSHA(ctx, repoDir, path)
		},
	}

	cleanup := func() {
		for _, d := range dirs {
			os.RemoveAll(d)
		}
	}
	return fns, cleanup
}

// AggregateForeignUpdateCandidates resolves update candidates for skills
// installed by gh-compatible tooling (no lockfile entry). Update detection
// compares the installed github-tree-sha against the upstream tree SHA at the
// latest ref, mirroring gh skill update. Pinned and local skills are reported
// with their own statuses and never fetched.
func AggregateForeignUpdateCandidates(installed []InstalledSkill, fns UpstreamFuncs) ([]UpdateCandidate, error) {
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
			source, _ := OwnerRepoFromURL(s.Meta.RepoURL)
			candidates = append(candidates, UpdateCandidate{
				SkillName: s.Name,
				Source:    source,
				Status:    StatusCheckFailed,
				Reason:    "missing github-path in skill metadata",
				Foreign:   true,
			})
		default:
			byRepo[s.Meta.RepoURL] = append(byRepo[s.Meta.RepoURL], s)
		}
	}

	for _, repoURL := range slices.Sorted(maps.Keys(byRepo)) {
		repoCandidates, err := resolveRepoCandidates(byRepo[repoURL], fns)
		if err != nil {
			// A failing repository must not block other skills (gh skill
			// update behaves the same way).
			for _, s := range byRepo[repoURL] {
				source, _ := OwnerRepoFromURL(s.Meta.RepoURL)
				candidates = append(candidates, UpdateCandidate{
					SkillName: s.Name,
					Source:    source,
					Status:    StatusCheckFailed,
					Reason:    err.Error(),
					Foreign:   true,
				})
			}
			continue
		}
		candidates = append(candidates, repoCandidates...)
	}

	sortCandidates(candidates)
	return candidates, nil
}

func resolveRepoCandidates(group []InstalledSkill, fns UpstreamFuncs) ([]UpdateCandidate, error) {
	repoURL := group[0].Meta.RepoURL
	source, err := OwnerRepoFromURL(repoURL)
	if err != nil {
		return nil, err
	}

	cloneDir, resolved, err := fns.FetchLatest(repoURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}

	candidates := make([]UpdateCandidate, 0, len(group))
	for _, s := range group {
		candidate := UpdateCandidate{
			SkillName:   s.Name,
			Source:      source,
			Ref:         resolved.Full,
			CurrentHash: s.Meta.TreeSHA,
			Foreign:     true,
		}

		treeSHA, err := fns.TreeSHA(cloneDir, s.Meta.Path)
		if err != nil {
			candidate.Status = StatusCheckFailed
			candidate.Reason = err.Error()
			candidates = append(candidates, candidate)
			continue
		}

		candidate.LatestHash = treeSHA
		candidate.Status = StatusUpToDate
		if treeSHA != s.Meta.TreeSHA {
			candidate.Status = StatusUpdateAvailable
		}

		stagedDir := filepath.Join(cloneDir, filepath.FromSlash(s.Meta.Path))
		if candidate.Status == StatusUpdateAvailable {
			stagedDir, err = copyToStaging(cloneDir, stagedDir)
			if err != nil {
				return nil, fmt.Errorf("stage %q: %w", s.Name, err)
			}
			if err := injectStagedMetadata(stagedDir, SourceMetadata{
				RepoURL: repoURL,
				Ref:     resolved.Full,
				TreeSHA: treeSHA,
				Path:    s.Meta.Path,
			}); err != nil {
				return nil, fmt.Errorf("stage metadata for %q: %w", s.Name, err)
			}
		}
		candidate.StagedDir = stagedDir
		candidates = append(candidates, candidate)
	}

	return candidates, nil
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

// injectStagedMetadata merges gh source metadata into the staged SKILL.md so
// the installed result stays updatable by gh skill.
func injectStagedMetadata(stagedDir string, meta SourceMetadata) error {
	path := filepath.Join(stagedDir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	injected, err := InjectSourceMetadata(string(data), meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(injected), 0o644)
}

func copyToStaging(parentDir, srcDir string) (string, error) {
	dir, err := os.MkdirTemp(parentDir, ".staged-*")
	if err != nil {
		return "", err
	}
	return dir, copyDir(srcDir, dir)
}
