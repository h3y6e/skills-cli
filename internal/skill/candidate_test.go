package skill_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/h3y6e/skolar/internal/testutil"
)

func TestResolveUpdateCandidates(t *testing.T) {
	t.Parallel()

	t.Run("up-to-date when hashes match", func(t *testing.T) {
		t.Parallel()

		lockEntries := map[string]lock.Entry{
			"my-skill": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "abc123",
			},
		}

		upstream := []skill.DiscoveredSkill{
			{
				Name:         "my-skill",
				Dir:          "/tmp/staged/my-skill",
				ComputedHash: "abc123",
			},
		}

		candidates := skill.ResolveUpdateCandidates(lockEntries, upstream)

		if len(candidates) != 1 {
			t.Fatalf("expected 1 candidate, got %d", len(candidates))
		}

		c := candidates[0]
		if c.SkillName != "my-skill" {
			t.Errorf("SkillName = %q, want %q", c.SkillName, "my-skill")
		}

		if c.Status != skill.StatusUpToDate {
			t.Errorf("Status = %q, want %q", c.Status, skill.StatusUpToDate)
		}

		if c.CurrentHash != "abc123" {
			t.Errorf("CurrentHash = %q, want %q", c.CurrentHash, "abc123")
		}

		if c.LatestHash != "abc123" {
			t.Errorf("LatestHash = %q, want %q", c.LatestHash, "abc123")
		}
	})

	t.Run("update-available when hashes differ", func(t *testing.T) {
		t.Parallel()

		lockEntries := map[string]lock.Entry{
			"my-skill": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "old-hash",
			},
		}

		upstream := []skill.DiscoveredSkill{
			{
				Name:         "my-skill",
				Dir:          "/tmp/staged/my-skill",
				ComputedHash: "new-hash",
			},
		}

		candidates := skill.ResolveUpdateCandidates(lockEntries, upstream)

		if len(candidates) != 1 {
			t.Fatalf("expected 1 candidate, got %d", len(candidates))
		}

		c := candidates[0]
		if c.Status != skill.StatusUpdateAvailable {
			t.Errorf("Status = %q, want %q", c.Status, skill.StatusUpdateAvailable)
		}

		if c.CurrentHash != "old-hash" {
			t.Errorf("CurrentHash = %q, want %q", c.CurrentHash, "old-hash")
		}

		if c.LatestHash != "new-hash" {
			t.Errorf("LatestHash = %q, want %q", c.LatestHash, "new-hash")
		}

		if c.StagedDir != "/tmp/staged/my-skill" {
			t.Errorf("StagedDir = %q, want %q", c.StagedDir, "/tmp/staged/my-skill")
		}
	})

	t.Run("multiple skills are resolved", func(t *testing.T) {
		t.Parallel()

		lockEntries := map[string]lock.Entry{
			"skill-a": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "hash-a",
			},
			"skill-b": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "hash-b-old",
			},
		}

		upstream := []skill.DiscoveredSkill{
			{Name: "skill-a", Dir: "/tmp/a", ComputedHash: "hash-a"},
			{Name: "skill-b", Dir: "/tmp/b", ComputedHash: "hash-b-new"},
		}

		candidates := skill.ResolveUpdateCandidates(lockEntries, upstream)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		byName := map[string]skill.UpdateCandidate{}
		for _, c := range candidates {
			byName[c.SkillName] = c
		}

		if byName["skill-a"].Status != skill.StatusUpToDate {
			t.Errorf("skill-a Status = %q, want %q", byName["skill-a"].Status, skill.StatusUpToDate)
		}

		if byName["skill-b"].Status != skill.StatusUpdateAvailable {
			t.Errorf("skill-b Status = %q, want %q", byName["skill-b"].Status, skill.StatusUpdateAvailable)
		}
	})

	t.Run("candidates are sorted by name", func(t *testing.T) {
		t.Parallel()

		lockEntries := map[string]lock.Entry{
			"zeta":  {Source: "o/r", SourceType: "github", ComputedHash: "h1"},
			"alpha": {Source: "o/r", SourceType: "github", ComputedHash: "h2"},
		}

		upstream := []skill.DiscoveredSkill{
			{Name: "zeta", Dir: "/tmp/z", ComputedHash: "h1"},
			{Name: "alpha", Dir: "/tmp/a", ComputedHash: "h2"},
		}

		candidates := skill.ResolveUpdateCandidates(lockEntries, upstream)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		if candidates[0].SkillName != "alpha" {
			t.Errorf("first candidate = %q, want %q", candidates[0].SkillName, "alpha")
		}

		if candidates[1].SkillName != "zeta" {
			t.Errorf("second candidate = %q, want %q", candidates[1].SkillName, "zeta")
		}
	})
}

func TestFilterCandidates(t *testing.T) {
	t.Parallel()

	candidates := []skill.UpdateCandidate{
		{SkillName: "a", Status: skill.StatusUpToDate},
		{SkillName: "b", Status: skill.StatusUpdateAvailable},
		{SkillName: "c", Status: skill.StatusCheckFailed},
	}

	t.Run("nil filter returns all", func(t *testing.T) {
		t.Parallel()

		result := skill.FilterCandidates(candidates, nil)
		if len(result) != 3 {
			t.Fatalf("expected 3, got %d", len(result))
		}
	})

	t.Run("empty filter returns all", func(t *testing.T) {
		t.Parallel()

		result := skill.FilterCandidates(candidates, []string{})
		if len(result) != 3 {
			t.Fatalf("expected 3, got %d", len(result))
		}
	})

	t.Run("filters to specified skills", func(t *testing.T) {
		t.Parallel()

		result := skill.FilterCandidates(candidates, []string{"b"})
		if len(result) != 1 {
			t.Fatalf("expected 1, got %d", len(result))
		}

		if result[0].SkillName != "b" {
			t.Errorf("SkillName = %q, want %q", result[0].SkillName, "b")
		}
	})

	t.Run("filters multiple skills", func(t *testing.T) {
		t.Parallel()

		result := skill.FilterCandidates(candidates, []string{"a", "c"})
		if len(result) != 2 {
			t.Fatalf("expected 2, got %d", len(result))
		}
	})
}

func collectLocked(entries map[string]lock.Entry, cloneFn skill.CloneFunc) ([]skill.UpdateCandidate, []skill.SkippedEntry) {
	return skill.CollectUpdateCandidates(entries, nil, cloneFn, skill.UpstreamFuncs{}, func(int, int) {})
}

func TestCollectUpdateCandidates(t *testing.T) {
	t.Parallel()

	t.Run("groups entries by source and resolves candidates", func(t *testing.T) {
		t.Parallel()

		srcA := t.TempDir()
		srcB := t.TempDir()

		testutil.WriteFiles(t, srcA, map[string]string{
			"skills/skill-x/SKILL.md": "# Skill X updated\n",
		})
		testutil.WriteFiles(t, srcB, map[string]string{
			"skills/skill-y/SKILL.md": "# Skill Y same\n",
		})

		discoveredA, err := skill.DiscoverSkills(srcA)
		if err != nil {
			t.Fatal(err)
		}

		discoveredB, err := skill.DiscoverSkills(srcB)
		if err != nil {
			t.Fatal(err)
		}

		entries := map[string]lock.Entry{
			"skill-x": {
				Source:       "h3y6e/skills-a",
				SourceType:   "github",
				ComputedHash: "old-hash-x",
			},
			"skill-y": {
				Source:       "h3y6e/skills-b",
				SourceType:   "github",
				ComputedHash: discoveredB[0].ComputedHash,
			},
		}

		cloneDirs := map[string]string{
			"h3y6e/skills-a": srcA,
			"h3y6e/skills-b": srcB,
		}
		cloneFn := func(source string) (string, error) {
			dir, ok := cloneDirs[source]
			if !ok {
				t.Fatalf("unexpected clone for source %q", source)
			}

			return dir, nil
		}

		candidates, _ := collectLocked(entries, cloneFn)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		byName := map[string]skill.UpdateCandidate{}
		for _, c := range candidates {
			byName[c.SkillName] = c
		}

		if byName["skill-x"].Status != skill.StatusUpdateAvailable {
			t.Errorf("skill-x Status = %q, want %q", byName["skill-x"].Status, skill.StatusUpdateAvailable)
		}

		if byName["skill-x"].LatestHash != discoveredA[0].ComputedHash {
			t.Errorf("skill-x LatestHash = %q, want %q", byName["skill-x"].LatestHash, discoveredA[0].ComputedHash)
		}

		if byName["skill-y"].Status != skill.StatusUpToDate {
			t.Errorf("skill-y Status = %q, want %q", byName["skill-y"].Status, skill.StatusUpToDate)
		}
	})

	t.Run("groups entries by source and ref", func(t *testing.T) {
		t.Parallel()

		srcMain := t.TempDir()
		srcFeature := t.TempDir()

		testutil.WriteFiles(t, srcMain, map[string]string{
			"skills/shared-main/SKILL.md": "# Main\n",
		})
		testutil.WriteFiles(t, srcFeature, map[string]string{
			"skills/shared-feature/SKILL.md": "# Feature\n",
		})

		discoveredMain, err := skill.DiscoverSkills(srcMain)
		if err != nil {
			t.Fatal(err)
		}

		discoveredFeature, err := skill.DiscoverSkills(srcFeature)
		if err != nil {
			t.Fatal(err)
		}

		entries := map[string]lock.Entry{
			"shared-main": {
				Source:       "h3y6e/spec-skills",
				Ref:          "main",
				SourceType:   "github",
				ComputedHash: "stale-main",
			},
			"shared-feature": {
				Source:       "h3y6e/spec-skills",
				Ref:          "feature/install",
				SourceType:   "github",
				ComputedHash: "stale-feature",
			},
		}

		cloneDirs := map[string]string{
			"h3y6e/spec-skills#main":            srcMain,
			"h3y6e/spec-skills#feature/install": srcFeature,
		}
		cloneFn := func(source string) (string, error) {
			dir, ok := cloneDirs[source]
			if !ok {
				t.Fatalf("unexpected clone for source %q", source)
			}

			return dir, nil
		}

		candidates, _ := collectLocked(entries, cloneFn)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		byName := map[string]skill.UpdateCandidate{}
		for _, candidate := range candidates {
			byName[candidate.SkillName] = candidate
		}

		if byName["shared-main"].Ref != "main" {
			t.Errorf("shared-main Ref = %q, want %q", byName["shared-main"].Ref, "main")
		}

		if byName["shared-main"].LatestHash != discoveredMain[0].ComputedHash {
			t.Errorf("shared-main LatestHash = %q, want %q", byName["shared-main"].LatestHash, discoveredMain[0].ComputedHash)
		}

		if byName["shared-feature"].Ref != "feature/install" {
			t.Errorf("shared-feature Ref = %q, want %q", byName["shared-feature"].Ref, "feature/install")
		}

		if byName["shared-feature"].LatestHash != discoveredFeature[0].ComputedHash {
			t.Errorf("shared-feature LatestHash = %q, want %q", byName["shared-feature"].LatestHash, discoveredFeature[0].ComputedHash)
		}
	})

	t.Run("returns sorted candidates across sources", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		testutil.WriteFiles(t, srcDir, map[string]string{
			"skills/zeta/SKILL.md":  "# Zeta\n",
			"skills/alpha/SKILL.md": "# Alpha\n",
		})

		discovered, err := skill.DiscoverSkills(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		hashByName := map[string]string{}
		for _, d := range discovered {
			hashByName[d.Name] = d.ComputedHash
		}

		entries := map[string]lock.Entry{
			"zeta":  {Source: "o/r", SourceType: "github", ComputedHash: hashByName["zeta"]},
			"alpha": {Source: "o/r", SourceType: "github", ComputedHash: hashByName["alpha"]},
		}

		cloneFn := func(_ string) (string, error) {
			return srcDir, nil
		}

		candidates, _ := collectLocked(entries, cloneFn)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		if candidates[0].SkillName != "alpha" {
			t.Errorf("first = %q, want 'alpha'", candidates[0].SkillName)
		}

		if candidates[1].SkillName != "zeta" {
			t.Errorf("second = %q, want 'zeta'", candidates[1].SkillName)
		}
	})

	t.Run("processes multiple sources concurrently", func(t *testing.T) {
		t.Parallel()

		srcA := t.TempDir()
		srcB := t.TempDir()

		testutil.WriteFiles(t, srcA, map[string]string{
			"skills/alpha/SKILL.md": "# Alpha\n",
		})
		testutil.WriteFiles(t, srcB, map[string]string{
			"skills/beta/SKILL.md": "# Beta\n",
		})

		entries := map[string]lock.Entry{
			"alpha": {Source: "h3y6e/skills-a", SourceType: "github", ComputedHash: "stale-a"},
			"beta":  {Source: "h3y6e/skills-b", SourceType: "github", ComputedHash: "stale-b"},
		}

		cloneDirs := map[string]string{
			"h3y6e/skills-a": srcA,
			"h3y6e/skills-b": srcB,
		}
		started := make(chan string, len(cloneDirs))
		release := make(chan struct{})

		cloneFn := func(source string) (string, error) {
			dir, ok := cloneDirs[source]
			if !ok {
				t.Fatalf("unexpected clone for source %q", source)
			}

			started <- source

			<-release

			return dir, nil
		}

		done := make(chan struct{})

		var candidates []skill.UpdateCandidate
		go func() {
			candidates, _ = collectLocked(entries, cloneFn)

			close(done)
		}()

		seen := map[string]bool{}
		for len(seen) < len(cloneDirs) {
			select {
			case source := <-started:
				seen[source] = true
			case <-time.After(200 * time.Millisecond):
				t.Fatalf("expected %d concurrent clone starts, got %d", len(cloneDirs), len(seen))
			}
		}

		close(release)

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("CollectUpdateCandidates() did not finish")
		}

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}
	})

	t.Run("when the recorded upstream SKILL.md is malformed, the check fails instead of reporting the skill removed", func(t *testing.T) {
		t.Parallel()
		// Arrange
		src := t.TempDir()
		testutil.WriteFiles(t, src, map[string]string{"skills/a/SKILL.md": "---\nname: a\ncompatibility: MCP (tools: x)\n---\n"})

		entries := map[string]lock.Entry{"a": {Source: "o/r", SourceType: "github", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}}

		// Act
		candidates, _ := collectLocked(entries, func(string) (string, error) { return src, nil })

		// Assert
		if len(candidates) != 1 || candidates[0].Status != skill.StatusCheckFailed {
			t.Errorf("candidates = %+v, want a check-failed", candidates)
		}
	})

	t.Run("when lockfile and gh skill installs share a source, a skill neither installed is listed once", func(t *testing.T) {
		t.Parallel()
		// Arrange
		src := t.TempDir()
		testutil.WriteFiles(t, src, map[string]string{
			"skills/a/SKILL.md": "# A\n",
			"skills/b/SKILL.md": "# B\n",
			"skills/c/SKILL.md": "# C\n",
		})

		entries := map[string]lock.Entry{"a": {Source: "o/r", SourceType: "github", SkillPath: "skills/a/SKILL.md"}}
		installed := []skill.InstalledSkill{{Name: "b", Meta: skill.SourceMetadata{RepoURL: "https://github.com/o/r", TreeSHA: "t", Path: "skills/b"}}}
		fns := skill.UpstreamFuncs{
			FetchLatest: func(string) (git.Snapshot, error) {
				return snapshotOf(t, src, "t"), nil
			},
		}

		// Act
		candidates, _ := skill.CollectUpdateCandidates(entries, installed, func(string) (string, error) { return src, nil }, fns, func(int, int) {})

		// Assert
		var news []string

		for _, c := range candidates {
			if c.Status == skill.StatusNew {
				news = append(news, c.SkillName)
			}
		}

		if !slices.Equal(news, []string{"c"}) {
			t.Errorf("new skills = %v, want [c]", news)
		}
	})

	t.Run("when one source cannot be cloned, only its skills fail the check", func(t *testing.T) {
		t.Parallel()
		// Arrange
		good := t.TempDir()
		testutil.WriteFiles(t, good, map[string]string{"skills/b/SKILL.md": "# B\n"})

		entries := map[string]lock.Entry{
			"a": {Source: "bad/source", SourceType: "github", ComputedHash: "h"},
			"b": {Source: "good/source", SourceType: "github", ComputedHash: "h"},
		}
		cloneFn := func(source string) (string, error) {
			if source == "bad/source" {
				return "", errors.New("clone failed")
			}

			return good, nil
		}

		// Act
		candidates, _ := collectLocked(entries, cloneFn)

		// Assert
		got := []skill.Status{candidates[0].Status, candidates[1].Status}
		if !slices.Equal(got, []skill.Status{skill.StatusCheckFailed, skill.StatusUpdateAvailable}) {
			t.Errorf("statuses = %v, want check-failed for a and update-available for b", got)
		}
	})

	t.Run("empty lockfile returns empty candidates", func(t *testing.T) {
		t.Parallel()

		cloneFn := func(_ string) (string, error) {
			t.Fatal("should not be called")
			return "", nil
		}

		candidates, _ := collectLocked(map[string]lock.Entry{}, cloneFn)

		if len(candidates) != 0 {
			t.Errorf("expected 0 candidates, got %d", len(candidates))
		}
	})

	t.Run("unsupported source types are skipped", func(t *testing.T) {
		t.Parallel()

		srcDir := t.TempDir()
		localSrcDir := t.TempDir()
		testutil.WriteFiles(t, srcDir, map[string]string{
			"skills/supported-skill/SKILL.md": "# Supported\n",
		})
		testutil.WriteFiles(t, localSrcDir, map[string]string{
			"skills/local-skill/SKILL.md": "# Local\n",
		})

		discovered, err := skill.DiscoverSkills(srcDir)
		if err != nil {
			t.Fatal(err)
		}

		localDiscovered, err := skill.DiscoverSkills(localSrcDir)
		if err != nil {
			t.Fatal(err)
		}

		entries := map[string]lock.Entry{
			"supported-skill": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: discovered[0].ComputedHash,
			},
			"local-skill": {
				Source:       localSrcDir,
				SourceType:   "local",
				ComputedHash: localDiscovered[0].ComputedHash,
			},
			"wellknown-skill": {
				Source:       "mintlify/docs",
				SourceType:   "well-known",
				ComputedHash: "wkhash",
			},
			"npm-skill": {
				Source:       "my-npm-pkg",
				SourceType:   "node_modules",
				ComputedHash: "npmhash",
			},
		}

		cloneFn := func(source string) (string, error) {
			if source == "h3y6e/spec-skills" {
				return srcDir, nil
			}

			if source == localSrcDir {
				return localSrcDir, nil
			}

			t.Fatalf("unexpected clone for source %q", source)

			return "", nil
		}

		candidates, skipped := collectLocked(entries, cloneFn)

		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(candidates))
		}

		candidateNames := []string{candidates[0].SkillName, candidates[1].SkillName}
		if !slices.Equal(candidateNames, []string{"local-skill", "supported-skill"}) {
			t.Errorf("candidate names = %v, want %v", candidateNames, []string{"local-skill", "supported-skill"})
		}

		if len(skipped) != 2 {
			t.Fatalf("expected 2 skipped, got %d", len(skipped))
		}

		skippedByName := map[string]skill.SkippedEntry{}
		for _, s := range skipped {
			skippedByName[s.SkillName] = s
		}

		wantSkipped := map[string]lock.SourceType{
			"wellknown-skill": "well-known",
			"npm-skill":       "node_modules",
		}
		for name, wantType := range wantSkipped {
			got, ok := skippedByName[name]
			if !ok {
				t.Errorf("expected %q in skipped entries", name)
				continue
			}

			if got.SourceType != wantType {
				t.Errorf("skipped %q SourceType = %q, want %q", name, got.SourceType, wantType)
			}
		}
	})
}

func TestResolveUpdateCandidatesBySkillPath(t *testing.T) {
	t.Parallel()

	at := func(name, path, hash string) skill.DiscoveredSkill {
		return skill.DiscoveredSkill{Name: name, Path: path, ComputedHash: hash, Dir: "/clone/" + path, Default: true}
	}

	type want struct {
		name   string
		status skill.Status
		path   string
	}

	for _, tc := range []struct {
		name     string
		entry    lock.Entry
		upstream []skill.DiscoveredSkill
		want     []want
	}{
		{
			"when the entry's skillPath exists upstream, the skill at that path is compared even if another directory shares its name",
			lock.Entry{SkillPath: ".claude/skills/dup/SKILL.md", ComputedHash: "b"},
			[]skill.DiscoveredSkill{at("dup", "skills/dup", "a"), at("dup", ".claude/skills/dup", "b")},
			[]want{{"dup", skill.StatusUpToDate, ".claude/skills/dup"}},
		},
		{
			"when the entry's skillPath is gone and one upstream skill has its name, the skill has moved there",
			lock.Entry{SkillPath: "old/dup/SKILL.md", ComputedHash: "a"},
			[]skill.DiscoveredSkill{at("dup", "skills/dup", "a")},
			[]want{{"dup", skill.StatusMoved, "skills/dup"}},
		},
		{
			"when the entry's skillPath is gone and no upstream skill has its name, the skill was removed upstream",
			lock.Entry{SkillPath: "skills/dup/SKILL.md", ComputedHash: "a"},
			[]skill.DiscoveredSkill{at("other", "skills/other", "a")},
			[]want{{"dup", skill.StatusRemoved, ""}, {"other", skill.StatusNew, "skills/other"}},
		},
		{
			"when an installed skill's path is another upstream skill's name, that skill is still new",
			lock.Entry{SkillPath: "other/SKILL.md", ComputedHash: "a"},
			[]skill.DiscoveredSkill{at("dup", "other", "a"), at("other", "skills/other", "b")},
			[]want{{"dup", skill.StatusUpToDate, "other"}, {"other", skill.StatusNew, "skills/other"}},
		},
		{
			"when the entry has no skillPath and two upstream skills share its name, the check fails as ambiguous",
			lock.Entry{ComputedHash: "a"},
			[]skill.DiscoveredSkill{at("dup", "skills/dup", "a"), at("dup", ".claude/skills/dup", "b")},
			[]want{{"dup", skill.StatusCheckFailed, ""}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			candidates := skill.ResolveUpdateCandidates(map[string]lock.Entry{"dup": tc.entry}, tc.upstream)

			// Assert
			got := make([]want, len(candidates))
			for i, c := range candidates {
				got[i] = want{c.SkillName, c.Status, c.Path}
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("candidates = %+v, want %+v", got, tc.want)
			}
		})
	}
}
