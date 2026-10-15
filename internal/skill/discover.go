package skill

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// DiscoveredSkill represents a skill found in an upstream repository.
type DiscoveredSkill struct {
	Name         string
	Dir          string
	Path         string // repo-relative directory of the skill, "." for the root
	ComputedHash string
	// Default reports whether vercel-labs/skills or gh skill finds the skill
	// without being told where to look.
	Default bool
}

// SkillMDPath returns the repo-relative path of the skill's SKILL.md, as
// vercel-labs/skills records it in skillPath.
func (s DiscoveredSkill) SkillMDPath() string {
	return path.Join(s.Path, "SKILL.md")
}

// skillsSegment is the directory name both tools look for skills under.
const skillsSegment = "skills"

// skipDirs are never searched, as in vercel-labs/skills.
var skipDirs = []string{".git", "node_modules", "dist", "build", "__pycache__"}

// vercelContainers are the directories vercel-labs/skills searches three
// levels deep before falling back to the whole repository.
var vercelContainers = []string{
	"skills", "skills/.curated", "skills/.experimental", "skills/.system",
	".agents/skills", ".claude/skills", ".cline/skills", ".codebuddy/skills", ".codex/skills",
	".commandcode/skills", ".continue/skills", ".factory/skills", ".github/skills", ".goose/skills",
	".grok/skills", ".iflow/skills", ".junie/skills", ".kilo/skills", ".kilocode/skills",
	".kimchi/skills", ".kiro/skills", ".minimax/skills", ".mux/skills", ".neovate/skills",
	".opencode/skills", ".openhands/skills", ".pi/skills", ".posit/assistant/skills", ".qoder/skills",
	".roo/skills", ".trae/skills", ".windsurf/skills", ".zcode/skills", ".zencoder/skills",
}

type skillDir struct {
	path        string // repo-relative, "." for the root
	name        string
	vercelValid bool
}

// DiscoverSkills finds every skill in a cloned repository that
// vercel-labs/skills (with --full-depth) or gh skill (with hidden
// directories) can install, in vercel-labs/skills' search order. Names may
// repeat across directories.
func DiscoverSkills(repoRoot string) ([]DiscoveredSkill, error) {
	all, err := findSkillDirs(repoRoot)
	if err != nil {
		return nil, err
	}

	vercel := vercelDefaults(repoRoot, all)

	defaults := map[string]bool{}
	for _, p := range vercel {
		defaults[p] = true
	}

	for p := range all {
		if ghDefault(p) {
			defaults[p] = true
		}
	}

	ordered := slices.Concat(vercel, slices.Sorted(func(yield func(string) bool) {
		for p := range all {
			if !yield(p) {
				return
			}
		}
	}))

	var picked []skillDir
	for _, p := range slices.Compact(ordered) {
		if d := all[p]; d.name != "" && !slices.ContainsFunc(picked, func(o skillDir) bool { return o.path == p }) {
			picked = append(picked, d)
		}
	}

	skills := make([]DiscoveredSkill, len(picked))
	errs := make([]error, len(picked))

	var wg sync.WaitGroup
	for i, d := range picked {
		wg.Go(func() {
			dir := filepath.Join(repoRoot, filepath.FromSlash(d.path))
			hash, err := ComputeHash(dir)
			skills[i] = DiscoveredSkill{Name: d.name, Dir: dir, Path: d.path, ComputedHash: hash, Default: defaults[d.path]}
			errs[i] = err
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return skills, nil
}

func findSkillDirs(repoRoot string) (map[string]skillDir, error) {
	dirs := map[string]skillDir{}
	err := filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}

		if info, err := os.Lstat(filepath.Join(p, "SKILL.md")); err == nil && info.Mode().IsRegular() {
			rel, err := filepath.Rel(repoRoot, p)
			if err != nil {
				return err
			}

			if dir, ok := readSkillDir(p, filepath.ToSlash(rel)); ok {
				dirs[dir.path] = dir
			}
		}

		if p != repoRoot && slices.Contains(skipDirs, d.Name()) {
			return filepath.SkipDir
		}

		return nil
	})

	return dirs, err
}

// readSkillDir reads a skill's SKILL.md, reporting false when it is
// unreadable or its frontmatter is malformed, which vercel-labs/skills skips.
func readSkillDir(dir, rel string) (skillDir, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return skillDir{}, false
	}

	raw, _, err := parseFrontmatter(string(data))
	if err != nil {
		return skillDir{}, false
	}

	name, _ := raw["name"].(string)
	if name != "" && (name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name) {
		return skillDir{}, false
	}

	description, _ := raw["description"].(string)
	metadata, _ := raw["metadata"].(map[string]any)
	internal := metadata["internal"] == true && !slices.Contains([]string{"1", "true"}, os.Getenv("INSTALL_INTERNAL_SKILLS"))

	d := skillDir{path: rel, name: name, vercelValid: name != "" && description != "" && !internal}
	if d.name == "" && rel != "." {
		d.name = path.Base(rel)
	}

	return d, true
}

// vercelDefaults returns, in search order, the skill directories
// vercel-labs/skills installs without --full-depth.
func vercelDefaults(repoRoot string, all map[string]skillDir) []string {
	if root, ok := all["."]; ok && root.vercelValid {
		return []string{"."}
	}

	search := &vercelSearch{repoRoot: repoRoot, all: all}
	search.walk(".", 1, 1)

	for _, c := range vercelContainers {
		search.walk(c, 1, 3)
	}

	for _, p := range pluginSkillDirs(repoRoot) {
		search.walk(p, 1, 1)
	}

	if len(search.found) > 0 {
		return search.found
	}

	var valid []string

	for p, d := range all {
		if d.vercelValid {
			valid = append(valid, p)
		}
	}

	slices.Sort(valid)

	return valid
}

// vercelSearch collects, in the order vercel-labs/skills searches them, the
// skills it installs by default.
type vercelSearch struct {
	repoRoot string
	all      map[string]skillDir
	found    []string
}

// walk searches the subdirectories of dir down to maxDepth, stopping at each
// skill directory.
func (s *vercelSearch) walk(dir string, depth, maxDepth int) {
	entries, _ := os.ReadDir(filepath.Join(s.repoRoot, filepath.FromSlash(dir)))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		child := path.Join(dir, e.Name())
		if d, ok := s.all[child]; ok {
			if d.vercelValid && !slices.Contains(s.found, child) {
				s.found = append(s.found, child)
			}

			continue
		}

		if depth < maxDepth && !slices.Contains(skipDirs, e.Name()) {
			s.walk(child, depth+1, maxDepth)
		}
	}
}

// pluginSkillDirs returns the directories a Claude plugin manifest declares
// skills in, as vercel-labs/skills reads them.
func pluginSkillDirs(repoRoot string) []string {
	var dirs []string

	add := func(base string, skills []string) {
		for _, s := range skills {
			if strings.HasPrefix(s, "./") {
				dirs = append(dirs, path.Dir(path.Join(base, s)))
			}
		}

		dirs = append(dirs, path.Join(base, skillsSegment))
	}

	var market struct {
		Metadata struct {
			PluginRoot string `json:"pluginRoot"`
		} `json:"metadata"`
		Plugins []struct {
			Source any      `json:"source"`
			Skills []string `json:"skills"`
		} `json:"plugins"`
	}
	if data, err := os.ReadFile(filepath.Join(repoRoot, ".claude-plugin", "marketplace.json")); err == nil && json.Unmarshal(data, &market) == nil {
		for _, p := range market.Plugins {
			source, ok := p.Source.(string)
			if p.Source != nil && (!ok || !strings.HasPrefix(source, "./")) {
				continue
			}

			add(path.Join(market.Metadata.PluginRoot, source), p.Skills)
		}
	}

	var plugin struct {
		Skills []string `json:"skills"`
	}
	if data, err := os.ReadFile(filepath.Join(repoRoot, ".claude-plugin", "plugin.json")); err == nil && json.Unmarshal(data, &plugin) == nil {
		add(".", plugin.Skills)
	}

	return dirs
}

// ghDefault reports whether gh skill install discovers a skill at the
// repo-relative path p without --allow-hidden-dirs.
func ghDefault(p string) bool {
	if p == "." {
		return false
	}

	segments := strings.Split(p, "/")
	for _, s := range segments {
		if strings.HasPrefix(s, ".") {
			return false
		}
	}

	n := len(segments)

	name := segments[n-1]
	switch {
	case n == 1:
		return name != skillsSegment && name != "plugins"
	case segments[n-2] == skillsSegment:
		return true
	case n >= 3 && segments[n-3] == skillsSegment:
		return true
	}

	return false
}
