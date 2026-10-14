package git

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// ResolvedRef is a git ref resolved against a remote repository.
type ResolvedRef struct {
	// Full is the fully-qualified ref ("refs/heads/main", "refs/tags/v1.0.0"),
	// or a raw commit SHA when the ref did not match any branch or tag.
	Full string
}

// ResolveRef resolves ref against a remote, preferring branches over tags.
// When ref is empty, it resolves the latest non-prerelease semver tag,
// falling back to the remote's default branch. This mirrors the version
// resolution of gh skill install (latest release, then default branch HEAD).
func ResolveRef(ctx context.Context, cloneURL, ref string) (ResolvedRef, error) {
	if ref != "" {
		return resolveExplicitRef(ctx, cloneURL, ref)
	}
	return resolveLatestRef(ctx, cloneURL)
}

func resolveExplicitRef(ctx context.Context, cloneURL, ref string) (ResolvedRef, error) {
	refs, err := lsRemote(ctx, cloneURL, "refs/heads/"+ref, "refs/tags/"+ref)
	if err != nil {
		return ResolvedRef{}, err
	}

	var branch, tag bool
	for _, name := range refs {
		switch name {
		case "refs/heads/" + ref:
			branch = true
		case "refs/tags/" + ref:
			tag = true
		}
	}

	switch {
	case branch:
		return ResolvedRef{Full: "refs/heads/" + ref}, nil
	case tag:
		return ResolvedRef{Full: "refs/tags/" + ref}, nil
	default:
		// Assume a commit SHA; fetching fails later if it does not exist.
		return ResolvedRef{Full: ref}, nil
	}
}

func resolveLatestRef(ctx context.Context, cloneURL string) (ResolvedRef, error) {
	tags, err := lsRemote(ctx, cloneURL, "refs/tags/*")
	if err != nil {
		return ResolvedRef{}, err
	}

	latestVersion, latestTag := "", ""
	for _, name := range tags {
		tag, found := strings.CutPrefix(name, "refs/tags/")
		if !found || strings.HasSuffix(tag, "^{}") {
			continue
		}
		version := tag
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		if !semver.IsValid(version) || semver.Prerelease(version) != "" {
			continue
		}
		if latestVersion == "" || semver.Compare(version, latestVersion) > 0 {
			latestVersion, latestTag = version, tag
		}
	}

	if latestTag != "" {
		return ResolvedRef{Full: "refs/tags/" + latestTag}, nil
	}

	branch, err := defaultBranch(ctx, cloneURL)
	if err != nil {
		return ResolvedRef{}, err
	}
	return ResolvedRef{Full: "refs/heads/" + branch}, nil
}

func defaultBranch(ctx context.Context, cloneURL string) (string, error) {
	out, err := runGit(ctx, "ls-remote", "--symref", cloneURL, "HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(out)) {
		if name, found := strings.CutPrefix(line, "ref: refs/heads/"); found {
			branch, _, _ := strings.Cut(name, "\t")
			return branch, nil
		}
	}
	return "", fmt.Errorf("git ls-remote %q: could not determine default branch", cloneURL)
}

// lsRemote returns the ref names advertised by the remote matching patterns.
func lsRemote(ctx context.Context, cloneURL string, patterns ...string) ([]string, error) {
	args := append([]string{"ls-remote", cloneURL}, patterns...)
	out, err := runGit(ctx, args...)
	if err != nil {
		return nil, err
	}

	var refs []string
	for line := range strings.Lines(string(out)) {
		_, name, found := strings.Cut(strings.TrimSpace(line), "\t")
		if found {
			refs = append(refs, name)
		}
	}
	return refs, nil
}

// TreeSHA returns the git tree object SHA for path at HEAD of repoDir.
func TreeSHA(ctx context.Context, repoDir, path string) (string, error) {
	rev := "HEAD:" + path
	if path == "." {
		rev = "HEAD^{tree}"
	}
	out, err := runGit(ctx, "-C", repoDir, "rev-parse", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
