package skill

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var shorthandPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// SourceRef holds a parsed skill source.
type SourceRef struct {
	Raw             string
	SourceType      string
	CanonicalSource string
	CloneURL        string
	Ref             string
}

// ParseSource parses a source string into a SourceRef.
// Supports GitHub/GitLab shorthand, SCP notation, and HTTP(S)/SSH URLs.
func ParseSource(raw string) (SourceRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SourceRef{}, fmt.Errorf("parse source: empty source")
	}

	rawWithoutFragment, ref := splitSourceFragment(raw)

	if looksLikeLocalPath(rawWithoutFragment) {
		return SourceRef{
			Raw:             raw,
			SourceType:      "local",
			CanonicalSource: filepath.Clean(rawWithoutFragment),
			CloneURL:        filepath.Clean(rawWithoutFragment),
			Ref:             ref,
		}, nil
	}

	if shorthandPattern.MatchString(rawWithoutFragment) {
		return SourceRef{
			Raw:             raw,
			SourceType:      "github",
			CanonicalSource: rawWithoutFragment,
			CloneURL:        "https://github.com/" + rawWithoutFragment + ".git",
			Ref:             ref,
		}, nil
	}

	if strings.HasPrefix(rawWithoutFragment, "git@") {
		return parseSCPSource(raw, rawWithoutFragment, ref)
	}

	u, err := url.Parse(rawWithoutFragment)
	if err != nil {
		return SourceRef{}, fmt.Errorf("parse source %q: %w", rawWithoutFragment, err)
	}

	if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "ssh" {
		return parseURLSource(raw, rawWithoutFragment, u, ref)
	}

	if u.Scheme == "file" {
		return SourceRef{
			Raw:             raw,
			SourceType:      "local",
			CanonicalSource: rawWithoutFragment,
			CloneURL:        rawWithoutFragment,
			Ref:             ref,
		}, nil
	}

	return SourceRef{}, fmt.Errorf("parse source %q: unsupported format", rawWithoutFragment)
}

func FormatSourceInput(source, ref string) string {
	if ref == "" {
		return source
	}
	return source + "#" + ref
}

// RepoURLForSource returns the canonical repository URL for a GitHub source
// in "owner/repo" form, matching the URL gh skill records in skill metadata.
func RepoURLForSource(canonicalSource string) string {
	return "https://github.com/" + canonicalSource
}

// OwnerRepoFromURL extracts the "owner/repo" form from a GitHub repository
// URL as recorded in skill metadata by gh-compatible tooling.
func OwnerRepoFromURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse repo url %q: %w", rawURL, err)
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") {
		return "", fmt.Errorf("parse repo url %q: only https://github.com is supported", rawURL)
	}

	path := strings.Trim(strings.TrimSuffix(u.EscapedPath(), ".git"), "/")
	segments := splitPathSegments(path)
	if len(segments) != 2 {
		return "", fmt.Errorf("parse repo url %q: expected owner/repo path", rawURL)
	}
	return strings.Join(segments, "/"), nil
}

func splitSourceFragment(raw string) (string, string) {
	hashIndex := strings.LastIndex(raw, "#")
	if hashIndex <= 0 || hashIndex == len(raw)-1 {
		return raw, ""
	}
	return raw[:hashIndex], raw[hashIndex+1:]
}

func parseSCPSource(raw, rawWithoutFragment, ref string) (SourceRef, error) {
	rest, found := strings.CutPrefix(rawWithoutFragment, "git@")
	if !found {
		return SourceRef{}, fmt.Errorf("parse source %q: invalid scp url", rawWithoutFragment)
	}

	host, path, ok := strings.Cut(rest, ":")
	if !ok || host == "" || path == "" {
		return SourceRef{}, fmt.Errorf("parse source %q: invalid scp url", rawWithoutFragment)
	}

	path = strings.TrimSuffix(path, ".git")

	if host == "github.com" || host == "gitlab.com" {
		return SourceRef{
			Raw:             raw,
			SourceType:      sourceTypeForHost(host),
			CanonicalSource: path,
			CloneURL:        rawWithoutFragment,
			Ref:             ref,
		}, nil
	}

	return SourceRef{
		Raw:             raw,
		SourceType:      "git",
		CanonicalSource: rawWithoutFragment,
		CloneURL:        rawWithoutFragment,
		Ref:             ref,
	}, nil
}

func parseURLSource(raw, rawWithoutFragment string, u *url.URL, ref string) (SourceRef, error) {
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(strings.TrimSuffix(u.EscapedPath(), ".git"), "/")
	if path == "" {
		return SourceRef{}, fmt.Errorf("parse source %q: missing repository path", rawWithoutFragment)
	}

	if host == "github.com" {
		segments := splitPathSegments(path)
		if len(segments) != 2 {
			return SourceRef{}, fmt.Errorf("parse source %q: unsupported github url path", rawWithoutFragment)
		}
		return SourceRef{
			Raw:             raw,
			SourceType:      "github",
			CanonicalSource: strings.Join(segments, "/"),
			CloneURL:        "https://" + host + "/" + strings.Join(segments, "/") + ".git",
			Ref:             ref,
		}, nil
	}

	if host == "gitlab.com" {
		if strings.Contains(path, "/-/") {
			return SourceRef{}, fmt.Errorf("parse source %q: unsupported gitlab url path", rawWithoutFragment)
		}
		segments := splitPathSegments(path)
		if len(segments) < 2 {
			return SourceRef{}, fmt.Errorf("parse source %q: missing repository path", rawWithoutFragment)
		}
		canonicalPath := strings.Join(segments, "/")
		return SourceRef{
			Raw:             raw,
			SourceType:      "gitlab",
			CanonicalSource: canonicalPath,
			CloneURL:        "https://" + host + "/" + canonicalPath + ".git",
			Ref:             ref,
		}, nil
	}

	return SourceRef{
		Raw:             raw,
		SourceType:      "git",
		CanonicalSource: rawWithoutFragment,
		CloneURL:        rawWithoutFragment,
		Ref:             ref,
	}, nil
}

func splitPathSegments(path string) []string {
	segments := strings.Split(path, "/")
	return slices.DeleteFunc(segments, func(segment string) bool {
		return segment == ""
	})
}

func sourceTypeForHost(host string) string {
	if host == "gitlab.com" {
		return "gitlab"
	}

	return "github"
}

func looksLikeLocalPath(raw string) bool {
	if filepath.IsAbs(raw) {
		return true
	}

	return raw == "." || raw == ".." ||
		strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../")
}

// SupportedSourceType reports whether the source type can be cloned/updated.
func SupportedSourceType(sourceType string) bool {
	switch sourceType {
	case "github", "gitlab", "git", "local":
		return true
	default:
		return false
	}
}
