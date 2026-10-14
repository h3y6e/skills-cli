package cmd_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/h3y6e/skolar/cmd"
	"github.com/h3y6e/skolar/internal/skill"
)

func TestResolveApprovalMode(t *testing.T) {
	t.Parallel()

	t.Run("apply when yes flag set", func(t *testing.T) {
		t.Parallel()

		mode := cmd.ResolveApprovalMode(true, false)
		if mode != cmd.ApprovalModeApply {
			t.Errorf("mode = %v, want %v", mode, cmd.ApprovalModeApply)
		}
	})

	t.Run("apply when tty is available", func(t *testing.T) {
		t.Parallel()

		mode := cmd.ResolveApprovalMode(false, true)
		if mode != cmd.ApprovalModeApply {
			t.Errorf("mode = %v, want %v", mode, cmd.ApprovalModeApply)
		}
	})

	t.Run("preview when non-tty and no flag", func(t *testing.T) {
		t.Parallel()

		mode := cmd.ResolveApprovalMode(false, false)
		if mode != cmd.ApprovalModePreview {
			t.Errorf("mode = %v, want %v", mode, cmd.ApprovalModePreview)
		}
	})
}

func TestResolveOutputMode(t *testing.T) {
	t.Parallel()

	t.Run("json when flag set", func(t *testing.T) {
		t.Parallel()

		mode := cmd.ResolveOutputMode(true)
		if mode != cmd.OutputModeJSON {
			t.Errorf("mode = %v, want %v", mode, cmd.OutputModeJSON)
		}
	})

	t.Run("human when flag not set", func(t *testing.T) {
		t.Parallel()

		mode := cmd.ResolveOutputMode(false)
		if mode != cmd.OutputModeHuman {
			t.Errorf("mode = %v, want %v", mode, cmd.OutputModeHuman)
		}
	})
}

func TestIsTTY(t *testing.T) {
	result := cmd.IsTTY()
	if result {
		t.Skip("running in a real TTY; cannot test non-TTY path")
	}
}

func TestCheckResultRendererHuman(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := cmd.NewCheckRenderer(cmd.OutputModeHuman, &buf)
	r.Render(cmd.CheckResult{
		SkillName:   "my-skill",
		Status:      skill.StatusUpToDate,
		Source:      "h3y6e/spec-skills",
		CurrentHash: "abc123",
		LatestHash:  "abc123",
	})

	got := buf.String()
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if !bytes.Contains([]byte(got), []byte("my-skill")) {
		t.Errorf("output should contain skill name, got: %q", got)
	}
}

func TestCheckResultRendererJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := cmd.NewCheckRenderer(cmd.OutputModeJSON, &buf)
	r.Render(cmd.CheckResult{
		SkillName:   "my-skill",
		Status:      skill.StatusUpdateAvailable,
		Source:      "h3y6e/spec-skills",
		CurrentHash: "old",
		LatestHash:  "new",
	})
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	var results []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	if results[0]["status"] != skill.StatusUpdateAvailable.String() {
		t.Errorf("status = %v, want %q", results[0]["status"], skill.StatusUpdateAvailable)
	}
}
