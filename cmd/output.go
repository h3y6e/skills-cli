package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/h3y6e/skills/internal/skill"
)

type ApprovalMode uint8

const (
	ApprovalModeApply ApprovalMode = iota
	ApprovalModePreview
)

type OutputMode uint8

const (
	OutputModeHuman OutputMode = iota
	OutputModeJSON
)

// IsTTY reports whether stdin is a terminal. Replaceable for testing.
var IsTTY = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func ResolveApprovalMode(yesFlag bool, isTTY bool) ApprovalMode {
	if yesFlag || isTTY {
		return ApprovalModeApply
	}
	return ApprovalModePreview
}

func ResolveOutputMode(jsonFlag bool) OutputMode {
	if jsonFlag {
		return OutputModeJSON
	}
	return OutputModeHuman
}

type CheckResult struct {
	SkillName   string       `json:"skillName"`
	Status      skill.Status `json:"status"`
	Source      string       `json:"source"`
	Ref         string       `json:"ref,omitempty"`
	CurrentHash string       `json:"currentHash"`
	LatestHash  string       `json:"latestHash,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

func NewCheckResult(candidate skill.UpdateCandidate) CheckResult {
	return CheckResult{
		SkillName:   candidate.SkillName,
		Status:      candidate.Status,
		Source:      candidate.Source,
		Ref:         candidate.Ref,
		CurrentHash: candidate.CurrentHash,
		LatestHash:  candidate.LatestHash,
		Reason:      candidate.Reason,
	}
}

type CheckRenderer struct {
	mode    OutputMode
	w       io.Writer
	results []CheckResult
}

func NewCheckRenderer(mode OutputMode, w io.Writer) *CheckRenderer {
	return &CheckRenderer{mode: mode, w: w}
}

func (r *CheckRenderer) Render(result CheckResult) {
	if r.mode == OutputModeJSON {
		r.results = append(r.results, result)
		return
	}

	switch result.Status {
	case skill.StatusUpToDate:
		fmt.Fprintf(r.w, "%s: %s\n", result.SkillName, result.Status)
	case skill.StatusUpdateAvailable:
		fmt.Fprintf(r.w, "%s: update available (%s -> %s)\n", result.SkillName, ShortHash(result.CurrentHash), ShortHash(result.LatestHash))
	case skill.StatusCheckFailed:
		fmt.Fprintf(r.w, "%s: check failed (%s)\n", result.SkillName, result.Reason)
	case skill.StatusPinned:
		fmt.Fprintf(r.w, "%s: pinned (%s)\n", result.SkillName, result.Reason)
	case skill.StatusLocal:
		fmt.Fprintf(r.w, "%s: local (%s)\n", result.SkillName, result.Reason)
	default:
		fmt.Fprintf(r.w, "%s: %s\n", result.SkillName, result.Status)
	}
}

func ShortHash(h string) string {
	if len(h) <= 7 {
		return h
	}
	return h[:7]
}

func (r *CheckRenderer) Flush() error {
	if r.mode != OutputModeJSON {
		return nil
	}

	data, err := json.MarshalIndent(r.results, "", "  ")
	if err != nil {
		return err
	}

	_, err = r.w.Write(append(data, '\n'))
	return err
}
