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

// ShallowClone clones cloneURL into destDir with depth=1 and optionally checks
// out the requested ref.
func ShallowClone(ctx context.Context, cloneURL, ref, destDir string) error {
	if ref == "" {
		return shallowCloneDefault(ctx, cloneURL, destDir)
	}

	return shallowCloneRef(ctx, cloneURL, ref, destDir)
}

func shallowCloneDefault(ctx context.Context, cloneURL, destDir string) error {
	_, err := runGit(ctx, "clone", "--depth", "1", "--", cloneURL, destDir)
	return err
}

func shallowCloneRef(ctx context.Context, cloneURL, ref, destDir string) error {
	if _, err := runGit(ctx, "init", destDir); err != nil {
		return err
	}

	if _, err := runGit(ctx, "-C", destDir, "remote", "add", "--", "origin", cloneURL); err != nil {
		return err
	}

	if _, err := runGit(ctx, "-C", destDir, "fetch", "--depth", "1", "--", "origin", ref); err != nil {
		return err
	}

	if _, err := runGit(ctx, "-C", destDir, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}

	return nil
}

// Stat counts the lines added and deleted, and the files changed, between two trees.
// Binary files count as changed files only; mode changes do not count.
type Stat struct {
	Added   int
	Deleted int
	Files   int
}

// DiffStat runs `git diff --no-index --numstat` from pathA to pathB.
func DiffStat(ctx context.Context, pathA, pathB string) (Stat, error) {
	cmd, err := gitCommand(ctx, "diff", "--no-index", "--numstat", "--", pathA, pathB)
	if err != nil {
		return Stat{}, err
	}

	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); err != nil && (!ok || exitErr.ExitCode() != 1) {
		return Stat{}, fmt.Errorf("git diff --no-index: %w", err)
	}

	var stat Stat

	for line := range strings.Lines(string(out)) {
		added, rest, _ := strings.Cut(line, "\t")
		deleted, _, _ := strings.Cut(rest, "\t")

		if added == "0" && deleted == "0" {
			continue
		}

		a, _ := strconv.Atoi(added)
		d, _ := strconv.Atoi(deleted)
		stat.Added += a
		stat.Deleted += d
		stat.Files++
	}

	return stat, nil
}
