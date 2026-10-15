package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// IsTTY reports whether stdin is a terminal. Replaceable for testing.
var IsTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func approve(cmd *cobra.Command, yes bool, question string) error {
	if yes {
		return nil
	}
	if !IsTTY() {
		return &cliError{codeConfirmationRequired, errors.New("confirmation required"), "re-run with --yes"}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", question)
	answers := make(chan string, 1)
	go func() {
		answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		answers <- answer
	}()
	ctx := cmd.Context()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case answer := <-answers:
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return &cliError{code: codeDeclined, err: errors.New("declined")}
		}
		return nil
	}
}

// writeRecord writes the --json result record, with err as its error field, and returns err.
func writeRecord(w io.Writer, key string, value any, err error) error {
	rec := map[string]any{key: value}
	if err != nil {
		rec["error"] = newErrorRecord(err)
	}
	data, marshalErr := json.MarshalIndent(rec, "", "  ")
	if marshalErr != nil {
		return errors.Join(err, marshalErr)
	}
	_, writeErr := fmt.Fprintln(w, string(data))
	return errors.Join(err, writeErr)
}

type checkResult struct {
	SkillName   string       `json:"skillName"`
	Status      skill.Status `json:"status"`
	Source      string       `json:"source"`
	Ref         string       `json:"ref,omitempty"`
	CurrentHash string       `json:"currentHash"`
	LatestHash  string       `json:"latestHash,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

func newCheckResult(candidate skill.UpdateCandidate) checkResult {
	return checkResult{
		SkillName:   candidate.SkillName,
		Status:      candidate.Status,
		Source:      candidate.Source,
		Ref:         candidate.Ref,
		CurrentHash: candidate.CurrentHash,
		LatestHash:  candidate.LatestHash,
		Reason:      candidate.Reason,
	}
}

func renderCheckResult(w io.Writer, result checkResult) {
	switch result.Status {
	case skill.StatusUpToDate:
		fmt.Fprintf(w, "%s: %s\n", result.SkillName, result.Status)
	case skill.StatusUpdateAvailable:
		fmt.Fprintf(w, "%s: update available (%s -> %s)\n", result.SkillName, shortHash(result.CurrentHash), shortHash(result.LatestHash))
	case skill.StatusCheckFailed:
		fmt.Fprintf(w, "%s: check failed (%s)\n", result.SkillName, result.Reason)
	case skill.StatusPinned:
		fmt.Fprintf(w, "%s: pinned (%s)\n", result.SkillName, result.Reason)
	case skill.StatusLocal:
		fmt.Fprintf(w, "%s: local (%s)\n", result.SkillName, result.Reason)
	default:
		fmt.Fprintf(w, "%s: %s\n", result.SkillName, result.Status)
	}
}

func shortHash(h string) string {
	if len(h) <= 7 {
		return h
	}
	return h[:7]
}
