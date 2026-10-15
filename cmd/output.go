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

// updateResult is an update --json result; renaming or retyping a field is breaking.
type updateResult struct {
	SkillName    string       `json:"skillName"`
	Status       skill.Status `json:"status"`
	Source       string       `json:"source"`
	Ref          string       `json:"ref,omitempty"`
	Path         string       `json:"path,omitempty"`
	PreviousPath string       `json:"previousPath,omitempty"`
	CurrentHash  string       `json:"currentHash,omitempty"`
	LatestHash   string       `json:"latestHash,omitempty"`
	Diff         *diffStat    `json:"diff,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Applied      bool         `json:"applied,omitempty"`
}

func newUpdateResult(c skill.UpdateCandidate) updateResult {
	return updateResult{
		SkillName:    c.SkillName,
		Status:       c.Status,
		Source:       c.Source,
		Ref:          c.Ref,
		Path:         c.Path,
		PreviousPath: c.PreviousPath,
		CurrentHash:  c.CurrentHash,
		LatestHash:   c.LatestHash,
		Reason:       c.Reason,
	}
}

type diffStat struct {
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
	Files   int `json:"files"`
}

func (d diffStat) String() string {
	unit := "files"
	if d.Files == 1 {
		unit = "file"
	}

	return fmt.Sprintf("+%d -%d (%d %s)", d.Added, d.Deleted, d.Files, unit)
}

func shortHash(h string) string {
	if len(h) <= 7 {
		return h
	}

	return h[:7]
}
