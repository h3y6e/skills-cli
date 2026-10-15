package skill_test

import (
	"path/filepath"
	"testing"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
)

func TestParseSource(t *testing.T) {
	relativeAbs, err := filepath.Abs("../spec-skills")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		raw     string
		want    skill.SourceRef
		wantErr bool
	}{
		{
			name: "github shorthand",
			raw:  "h3y6e/spec-skills",
			want: skill.SourceRef{
				Raw:             "h3y6e/spec-skills",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "https://github.com/h3y6e/spec-skills.git",
			},
		},
		{
			name: "github shorthand with ref",
			raw:  "h3y6e/spec-skills#feature/install",
			want: skill.SourceRef{
				Raw:             "h3y6e/spec-skills#feature/install",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "https://github.com/h3y6e/spec-skills.git",
				Ref:             "feature/install",
			},
		},
		{
			name: "github https url",
			raw:  "https://github.com/h3y6e/spec-skills",
			want: skill.SourceRef{
				Raw:             "https://github.com/h3y6e/spec-skills",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "https://github.com/h3y6e/spec-skills.git",
			},
		},
		{
			name: "github https url with ref",
			raw:  "https://github.com/h3y6e/spec-skills.git#release-2026",
			want: skill.SourceRef{
				Raw:             "https://github.com/h3y6e/spec-skills.git#release-2026",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "https://github.com/h3y6e/spec-skills.git",
				Ref:             "release-2026",
			},
		},
		{
			name: "gitlab https url",
			raw:  "https://gitlab.com/h3y6e/repo",
			want: skill.SourceRef{
				Raw:             "https://gitlab.com/h3y6e/repo",
				SourceType:      "gitlab",
				CanonicalSource: "h3y6e/repo",
				CloneURL:        "https://gitlab.com/h3y6e/repo.git",
			},
		},
		{
			name: "gitlab https url with ref",
			raw:  "https://gitlab.com/h3y6e/repo#feature/install",
			want: skill.SourceRef{
				Raw:             "https://gitlab.com/h3y6e/repo#feature/install",
				SourceType:      "gitlab",
				CanonicalSource: "h3y6e/repo",
				CloneURL:        "https://gitlab.com/h3y6e/repo.git",
				Ref:             "feature/install",
			},
		},
		{
			name: "github ssh url",
			raw:  "git@github.com:h3y6e/spec-skills.git",
			want: skill.SourceRef{
				Raw:             "git@github.com:h3y6e/spec-skills.git",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "git@github.com:h3y6e/spec-skills.git",
			},
		},
		{
			name: "github ssh url with ref",
			raw:  "git@github.com:h3y6e/spec-skills.git#feature/install",
			want: skill.SourceRef{
				Raw:             "git@github.com:h3y6e/spec-skills.git#feature/install",
				SourceType:      "github",
				CanonicalSource: "h3y6e/spec-skills",
				CloneURL:        "git@github.com:h3y6e/spec-skills.git",
				Ref:             "feature/install",
			},
		},
		{
			name: "generic git url",
			raw:  "ssh://git@example.com/team/skills.git",
			want: skill.SourceRef{
				Raw:             "ssh://git@example.com/team/skills.git",
				SourceType:      "git",
				CanonicalSource: "ssh://git@example.com/team/skills.git",
				CloneURL:        "ssh://git@example.com/team/skills.git",
			},
		},
		{
			name: "generic git url with ref",
			raw:  "ssh://git@example.com/team/skills.git#release-2026",
			want: skill.SourceRef{
				Raw:             "ssh://git@example.com/team/skills.git#release-2026",
				SourceType:      "git",
				CanonicalSource: "ssh://git@example.com/team/skills.git",
				CloneURL:        "ssh://git@example.com/team/skills.git",
				Ref:             "release-2026",
			},
		},
		{
			name: "self-hosted https url",
			raw:  "https://gitea.example.com/org/repo",
			want: skill.SourceRef{
				Raw:             "https://gitea.example.com/org/repo",
				SourceType:      "git",
				CanonicalSource: "https://gitea.example.com/org/repo",
				CloneURL:        "https://gitea.example.com/org/repo",
			},
		},
		{
			name:    "invalid source",
			raw:     "not a source",
			wantErr: true,
		},
		{
			name:    "github blob url is unsupported",
			raw:     "https://github.com/h3y6e/spec-skills/blob/main/README.md#L10",
			wantErr: true,
		},
		{
			name: "file url for local testing",
			raw:  "file:///tmp/bare-repo",
			want: skill.SourceRef{
				Raw:             "file:///tmp/bare-repo",
				SourceType:      "local",
				CanonicalSource: "file:///tmp/bare-repo",
				CloneURL:        "file:///tmp/bare-repo",
			},
		},
		{
			name: "file url with ref",
			raw:  "file:///tmp/bare-repo#topic",
			want: skill.SourceRef{
				Raw:             "file:///tmp/bare-repo#topic",
				SourceType:      "local",
				CanonicalSource: "file:///tmp/bare-repo",
				CloneURL:        "file:///tmp/bare-repo",
				Ref:             "topic",
			},
		},
		{
			name: "absolute local path",
			raw:  "/tmp/spec-skills",
			want: skill.SourceRef{
				Raw:             "/tmp/spec-skills",
				SourceType:      "local",
				CanonicalSource: "/tmp/spec-skills",
				CloneURL:        "/tmp/spec-skills",
			},
		},
		{
			name: "relative local path, cloned from the working directory",
			raw:  "../spec-skills#feature/install",
			want: skill.SourceRef{
				Raw:             "../spec-skills#feature/install",
				SourceType:      "local",
				CanonicalSource: "../spec-skills",
				CloneURL:        relativeAbs,
				Ref:             "feature/install",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := skill.ParseSource(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseSource() error = nil, want non-nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseSource() error = %v", err)
			}

			if got != tt.want {
				t.Fatalf("ParseSource() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSourceInput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		entry lock.Entry
		want  string
	}{
		{"when the entry is a GitLab source, the input names gitlab.com", lock.Entry{Source: "team/repo", SourceType: "gitlab", Ref: "v1"}, "https://gitlab.com/team/repo#v1"},
		{"when the entry is a GitHub source, the input is its shorthand", lock.Entry{Source: "o/r", SourceType: "github"}, "o/r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Act
			got := skill.SourceInput(tc.entry)

			// Assert
			if got != tc.want {
				t.Errorf("SourceInput() = %q, want %q", got, tc.want)
			}

			if ref, err := skill.ParseSource(got); err != nil || ref.SourceType != tc.entry.SourceType {
				t.Errorf("ParseSource(%q) = %+v, %v; want source type %s", got, ref, err, tc.entry.SourceType)
			}
		})
	}
}
