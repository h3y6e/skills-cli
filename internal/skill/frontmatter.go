package skill

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontmatterDelimiter = "---"

// SourceMetadata describes where an installed skill came from, as recorded
// in SKILL.md frontmatter by gh-compatible tooling (gh skill).
type SourceMetadata struct {
	RepoURL   string // metadata.github-repo, e.g. "https://github.com/owner/repo"
	Ref       string // metadata.github-ref, e.g. "refs/heads/main", "refs/tags/v1.0.0", or a commit SHA
	TreeSHA   string // metadata.github-tree-sha
	Path      string // metadata.github-path, e.g. "skills/foo"
	Pinned    string // metadata.github-pinned (empty = unpinned)
	LocalPath string // metadata.local-path
}

// IsGitHub reports whether the skill was installed from a GitHub repository.
func (m SourceMetadata) IsGitHub() bool { return m.RepoURL != "" }

// IsLocal reports whether the skill was installed from a local directory.
func (m SourceMetadata) IsLocal() bool { return m.LocalPath != "" }

// ParseSourceMetadata reads gh-compatible source metadata from SKILL.md content.
// Content without frontmatter yields zero metadata and no error.
func ParseSourceMetadata(content string) (SourceMetadata, error) {
	raw, _, err := parseFrontmatter(content)
	if err != nil {
		return SourceMetadata{}, err
	}

	meta, _ := raw["metadata"].(map[string]interface{})
	return SourceMetadata{
		RepoURL:   stringValue(meta, "github-repo"),
		Ref:       stringValue(meta, "github-ref"),
		TreeSHA:   stringValue(meta, "github-tree-sha"),
		Path:      stringValue(meta, "github-path"),
		Pinned:    stringValue(meta, "github-pinned"),
		LocalPath: stringValue(meta, "local-path"),
	}, nil
}

// InjectSourceMetadata merges gh-compatible source metadata into SKILL.md
// content, preserving other frontmatter keys and the body. A frontmatter
// block is created when the content has none.
func InjectSourceMetadata(content string, m SourceMetadata) (string, error) {
	raw, body, err := parseFrontmatter(content)
	if err != nil {
		return "", err
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}

	meta, _ := raw["metadata"].(map[string]interface{})
	if meta == nil {
		meta = make(map[string]interface{})
	}

	// Legacy keys written by older gh versions.
	delete(meta, "github-owner")
	delete(meta, "github-sha")

	if m.IsGitHub() {
		delete(meta, "local-path")
		meta["github-repo"] = m.RepoURL
		meta["github-ref"] = m.Ref
		meta["github-tree-sha"] = m.TreeSHA
		meta["github-path"] = m.Path
		if m.Pinned != "" {
			meta["github-pinned"] = m.Pinned
		} else {
			delete(meta, "github-pinned")
		}
	} else if m.IsLocal() {
		for _, key := range []string{"github-repo", "github-ref", "github-tree-sha", "github-path", "github-pinned"} {
			delete(meta, key)
		}
		meta["local-path"] = m.LocalPath
	}
	raw["metadata"] = meta

	return serializeFrontmatter(raw, body)
}

func stringValue(meta map[string]interface{}, key string) string {
	value, _ := meta[key].(string)
	return value
}

// parseFrontmatter splits SKILL.md content into its YAML frontmatter map and
// body. Without a frontmatter block it returns a nil map and the full content
// as body. The delimiter handling intentionally mirrors gh skill (cli/cli)
// so injected files stay byte-compatible.
func parseFrontmatter(content string) (map[string]interface{}, string, error) {
	trimmed := strings.TrimLeft(content, "\r\n")
	if !strings.HasPrefix(trimmed, frontmatterDelimiter) {
		return nil, content, nil
	}

	rest := strings.TrimLeft(trimmed[len(frontmatterDelimiter):], "\r\n")
	endIndex := strings.Index(rest, "\n"+frontmatterDelimiter)
	if endIndex == -1 {
		return nil, content, nil
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(rest[:endIndex]), &raw); err != nil {
		return nil, "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	body := strings.TrimLeft(rest[endIndex+len("\n"+frontmatterDelimiter):], "\r\n")
	return raw, body, nil
}

func serializeFrontmatter(raw map[string]interface{}, body string) (string, error) {
	yamlBytes, err := yaml.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("serialize frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(frontmatterDelimiter + "\n")
	buf.Write(yamlBytes)
	buf.WriteString(frontmatterDelimiter + "\n")
	if body != "" {
		buf.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			buf.WriteString("\n")
		}
	}
	return buf.String(), nil
}
