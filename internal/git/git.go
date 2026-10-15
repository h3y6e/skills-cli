// Package git runs the git operations skolar needs against remote repositories.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func gitCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, gitPath, args...)

	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")

	return cmd, nil
}

func runGit(ctx context.Context, args ...string) ([]byte, error) {
	cmd, err := gitCommand(ctx, args...)
	if err != nil {
		return nil, err
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %v: %w\n%s", args, err, out)
	}

	return out, nil
}

// NumStat counts the lines added and deleted from pathA to pathB, with
// `git diff --no-index --numstat`. Binary files add no lines.
func NumStat(ctx context.Context, pathA, pathB string) (added, deleted int, err error) {
	cmd, err := gitCommand(ctx, "diff", "--no-index", "--numstat", "--", pathA, pathB)
	if err != nil {
		return 0, 0, err
	}

	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); err != nil && (!ok || exitErr.ExitCode() != 1) {
		return 0, 0, fmt.Errorf("git diff --no-index: %w", err)
	}

	for line := range strings.Lines(string(out)) {
		a, rest, _ := strings.Cut(line, "\t")
		d, _, _ := strings.Cut(rest, "\t")
		n, _ := strconv.Atoi(a)
		m, _ := strconv.Atoi(d)
		added += n
		deleted += m
	}

	return added, deleted, nil
}
