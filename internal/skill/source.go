package skill

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/h3y6e/skolar/internal/lock"
)

var shorthandPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// SourceRef holds a parsed skill source.
type SourceRef struct {
	Raw             string
	SourceType      lock.SourceType
	CanonicalSource string
	CloneURL        string
	Ref             string
}

// ParseSource parses a source string into a SourceRef.
// Supports GitHub/GitLab shorthand, SCP notation, and HTTP(S)/SSH URLs.
func ParseSource(raw string) (SourceRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SourceRef{}, errors.New("parse source: empty source")
	}

	location, ref := splitSourceFragment(raw)

	sourceType, canonical, cloneURL, err := parseLocation(location)
	if err != nil {
		return SourceRef{}, err
	}

	return SourceRef{Raw: raw, SourceType: sourceType, CanonicalSource: canonical, CloneURL: cloneURL, Ref: ref}, nil
}

func parseLocation(location string) (lock.SourceType, string, string, error) {
	if looksLikeLocalPath(location) {
		return lock.SourceLocal, filepath.Clean(location), filepath.Clean(location), nil
	}

	if shorthandPattern.MatchString(location) {
		return lock.SourceGitHub, location, "https://github.com/" + location + ".git", nil
	}

	if rest, found := strings.CutPrefix(location, "git@"); found {
		return parseSCPLocation(location, rest)
	}

	u, err := url.Parse(location)
	if err != nil {
		return "", "", "", fmt.Errorf("parse source %q: %w", location, err)
	}

	switch u.Scheme {
	case "http", "https", "ssh":
		return parseURLLocation(location, u)
	case "file":
		return lock.SourceLocal, location, location, nil
	}

	return "", "", "", fmt.Errorf("parse source %q: unsupported format", location)
}

// SourceInput returns the add argument that reinstalls a lock entry.
func SourceInput(entry lock.Entry) string {
	source := entry.Source
	if entry.SourceType == lock.SourceGitLab {
		source = "https://gitlab.com/" + source
	}

	return FormatSourceInput(source, entry.Ref)
}

// FormatSourceInput joins source and an optional ref into an add argument.
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

func parseSCPLocation(location, rest string) (lock.SourceType, string, string, error) {
	host, path, ok := strings.Cut(rest, ":")
	if !ok || host == "" || path == "" {
		return "", "", "", fmt.Errorf("parse source %q: invalid scp url", location)
	}

	switch host {
	case "github.com":
		return lock.SourceGitHub, strings.TrimSuffix(path, ".git"), location, nil
	case "gitlab.com":
		return lock.SourceGitLab, strings.TrimSuffix(path, ".git"), location, nil
	}

	return lock.SourceGit, location, location, nil
}

func parseURLLocation(location string, u *url.URL) (lock.SourceType, string, string, error) {
	host := strings.ToLower(u.Hostname())

	path := strings.Trim(strings.TrimSuffix(u.EscapedPath(), ".git"), "/")
	if path == "" {
		return "", "", "", fmt.Errorf("parse source %q: missing repository path", location)
	}

	segments := splitPathSegments(path)

	switch host {
	case "github.com":
		if len(segments) != 2 {
			return "", "", "", fmt.Errorf("parse source %q: unsupported github url path", location)
		}

		canonical := strings.Join(segments, "/")

		return lock.SourceGitHub, canonical, "https://" + host + "/" + canonical + ".git", nil
	case "gitlab.com":
		if strings.Contains(path, "/-/") {
			return "", "", "", fmt.Errorf("parse source %q: unsupported gitlab url path", location)
		}

		if len(segments) < 2 {
			return "", "", "", fmt.Errorf("parse source %q: missing repository path", location)
		}

		canonical := strings.Join(segments, "/")

		return lock.SourceGitLab, canonical, "https://" + host + "/" + canonical + ".git", nil
	}

	return lock.SourceGit, location, location, nil
}

func splitPathSegments(path string) []string {
	segments := strings.Split(path, "/")

	return slices.DeleteFunc(segments, func(segment string) bool {
		return segment == ""
	})
}

func looksLikeLocalPath(raw string) bool {
	if filepath.IsAbs(raw) {
		return true
	}

	return raw == "." || raw == ".." ||
		strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../")
}
