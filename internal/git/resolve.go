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
	// Object is the object the remote advertises for Full: a commit, a tag
	// object for an annotated tag, or Full itself for a raw SHA.
	Object string
}

// ResolveLatest resolves ref against a remote, preferring branches over
// tags. When ref is empty, it resolves the latest non-prerelease semver tag,
// falling back to the remote's default branch, as gh skill install does.
func ResolveLatest(ctx context.Context, cloneURL, ref string) (ResolvedRef, error) {
	if ref != "" {
		return resolveExplicitRef(ctx, cloneURL, ref)
	}

	refs, err := lsRemote(ctx, cloneURL, "HEAD", "refs/tags/*")
	if err != nil {
		return ResolvedRef{}, err
	}

	if tag := latestTag(refs); tag != "" {
		return ResolvedRef{Full: tag, Object: refs.objects[tag]}, nil
	}

	return refs.head(cloneURL)
}

// ResolveDefault resolves ref against a remote, preferring branches over
// tags, or the remote's default branch when ref is empty, as git clone does.
func ResolveDefault(ctx context.Context, cloneURL, ref string) (ResolvedRef, error) {
	if ref != "" {
		return resolveExplicitRef(ctx, cloneURL, ref)
	}

	refs, err := lsRemote(ctx, cloneURL, "HEAD")
	if err != nil {
		return ResolvedRef{}, err
	}

	return refs.head(cloneURL)
}

func resolveExplicitRef(ctx context.Context, cloneURL, ref string) (ResolvedRef, error) {
	candidates := []string{"refs/heads/" + ref, "refs/tags/" + ref}
	if strings.HasPrefix(ref, "refs/") {
		candidates = append([]string{ref}, candidates...)
	}

	refs, err := lsRemote(ctx, cloneURL, candidates...)
	if err != nil {
		return ResolvedRef{}, err
	}

	for _, full := range candidates {
		if object, ok := refs.objects[full]; ok {
			return ResolvedRef{Full: full, Object: object}, nil
		}
	}

	// Assume a commit SHA; fetching fails later if it does not exist.
	return ResolvedRef{Full: ref, Object: ref}, nil
}

func latestTag(refs remoteRefs) string {
	latestVersion, latest := "", ""

	for name := range refs.objects {
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
			latestVersion, latest = version, name
		}
	}

	return latest
}

type remoteRefs struct {
	headTarget string
	objects    map[string]string
}

func (r remoteRefs) head(cloneURL string) (ResolvedRef, error) {
	if r.headTarget == "" {
		return ResolvedRef{}, fmt.Errorf("git ls-remote %q: could not determine default branch", cloneURL)
	}

	return ResolvedRef{Full: r.headTarget, Object: r.objects["HEAD"]}, nil
}

// lsRemote returns the refs the remote advertises matching patterns, with
// the branch HEAD points to.
func lsRemote(ctx context.Context, cloneURL string, patterns ...string) (remoteRefs, error) {
	args := append([]string{"ls-remote", "--symref", "--", cloneURL}, patterns...)

	out, err := runGit(ctx, args...)
	if err != nil {
		return remoteRefs{}, err
	}

	refs := remoteRefs{objects: map[string]string{}}

	for line := range strings.Lines(string(out)) {
		left, name, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found {
			continue
		}

		if target, ok := strings.CutPrefix(left, "ref: "); ok {
			if name == "HEAD" {
				refs.headTarget = target
			}

			continue
		}

		refs.objects[name] = left
	}

	return refs, nil
}
