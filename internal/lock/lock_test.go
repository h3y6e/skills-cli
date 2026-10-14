package lock_test

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/lock"
)

//go:embed testdata/skills-lock.json
var lockFixture []byte

func TestLockPathRelativeAlwaysAtCWD(t *testing.T) {
	cases := []string{
		filepath.Join(".agents", "skills"),
		filepath.Join("a", "b"),
		filepath.Join("a", "b", "c"),
		filepath.Join("a", "b", "c", "d"),
	}
	for _, destDir := range cases {
		l := lock.NewLayout(destDir)
		if got := l.LockPath(); got != lock.FileName {
			t.Errorf("LockPath(%q) = %q, want %q", destDir, got, lock.FileName)
		}
	}
}

func TestReadWriteFileRoundTrip(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	path := lock.FilePath(destDir)

	want := lock.File{
		Version: 1,
		Skills: map[string]lock.Entry{
			"spec-plan": {
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "abc123",
				Dest:         destDir,
			},
		},
	}

	if err := lock.WriteFile(path, want); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := lock.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got.Version != want.Version {
		t.Fatalf("Version = %d, want %d", got.Version, want.Version)
	}

	if got.Skills["spec-plan"] != want.Skills["spec-plan"] {
		t.Fatalf("entry = %#v, want %#v", got.Skills["spec-plan"], want.Skills["spec-plan"])
	}
}

func TestReadFileFixture(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), ".agents", "skills")
	path := lock.FilePath(destDir)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, lockFixture, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := lock.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1", got.Version)
	}

	tests := []struct {
		name  string
		entry lock.Entry
	}{
		{
			name: "skill-github",
			entry: lock.Entry{
				Source:       "h3y6e/spec-skills",
				SourceType:   "github",
				ComputedHash: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			},
		},
		{
			name: "skill-gitlab",
			entry: lock.Entry{
				Source:       "h3y6e/repo",
				SourceType:   "gitlab",
				ComputedHash: "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
			},
		},
		{
			name: "skill-git",
			entry: lock.Entry{
				Source:       "https://git.example.com/team/skills.git",
				SourceType:   "git",
				ComputedHash: "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
			},
		},
		{
			name: "skill-local",
			entry: lock.Entry{
				Source:       "/home/user/my-skills",
				SourceType:   "local",
				ComputedHash: "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5",
			},
		},
		{
			name: "skill-well-known",
			entry: lock.Entry{
				Source:       "mintlify/docs",
				SourceType:   "well-known",
				ComputedHash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "skill-node-modules",
			entry: lock.Entry{
				Source:       "my-npm-pkg",
				SourceType:   "node_modules",
				ComputedHash: "7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := got.Skills[tt.name]
			if !ok {
				t.Fatalf("entry %q missing", tt.name)
			}
			if got != tt.entry {
				t.Fatalf("entry = %#v, want %#v", got, tt.entry)
			}
		})
	}

	raw, _ := json.Marshal(got)
	var roundTrip lock.File
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("JSON round-trip failed: %v", err)
	}
}

func TestEffectiveDest(t *testing.T) {
	t.Parallel()

	t.Run("returns entry dest when set", func(t *testing.T) {
		t.Parallel()
		entry := lock.Entry{Dest: "custom/path"}
		if got := lock.EffectiveDest(entry); got != "custom/path" {
			t.Errorf("EffectiveDest() = %q, want %q", got, "custom/path")
		}
	})

	t.Run("returns DefaultDestDir when dest is empty", func(t *testing.T) {
		t.Parallel()
		entry := lock.Entry{}
		if got := lock.EffectiveDest(entry); got != lock.DefaultDestDir {
			t.Errorf("EffectiveDest() = %q, want %q", got, lock.DefaultDestDir)
		}
	})
}

func TestFilterEntriesByDest(t *testing.T) {
	t.Parallel()

	entries := map[string]lock.Entry{
		"alpha": {Source: "src-a", SourceType: "git", ComputedHash: "aaa", Dest: ".agents/skills"},
		"beta":  {Source: "src-b", SourceType: "git", ComputedHash: "bbb", Dest: "custom/path"},
		"gamma": {Source: "src-c", SourceType: "git", ComputedHash: "ccc"}, // empty dest → DefaultDestDir
	}

	t.Run("matches entries with explicit default dest and empty dest", func(t *testing.T) {
		t.Parallel()
		got := lock.FilterEntriesByDest(entries, ".agents/skills")
		if len(got) != 2 {
			t.Fatalf("expected 2 entries, got %d: %v", len(got), got)
		}
		if _, ok := got["alpha"]; !ok {
			t.Error("expected alpha (explicit default dest)")
		}
		if _, ok := got["gamma"]; !ok {
			t.Error("expected gamma (empty dest → default)")
		}
	})

	t.Run("matches entries with custom dest", func(t *testing.T) {
		t.Parallel()
		got := lock.FilterEntriesByDest(entries, "custom/path")
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d: %v", len(got), got)
		}
		if _, ok := got["beta"]; !ok {
			t.Error("expected beta")
		}
	})

	t.Run("does not match empty-dest entries against custom dest", func(t *testing.T) {
		t.Parallel()
		got := lock.FilterEntriesByDest(entries, "other/path")
		if len(got) != 0 {
			t.Fatalf("expected 0 entries, got %d: %v", len(got), got)
		}
	})

	t.Run("preserves original entry without mutating dest", func(t *testing.T) {
		t.Parallel()
		got := lock.FilterEntriesByDest(entries, ".agents/skills")
		if gamma, ok := got["gamma"]; ok && gamma.Dest != "" {
			t.Errorf("gamma.Dest should remain empty, got %q", gamma.Dest)
		}
	})
}
