// Package git runs the git operations skolar needs against remote repositories.
package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// DiffNoIndex runs `git diff --no-index` between two paths.
// Returns the diff output, whether a diff exists, and any error.
// Exit code 1 indicates a diff (not an error).
func DiffNoIndex(ctx context.Context, pathA, pathB string) (string, bool, error) {
	cmd, err := gitCommand(ctx, "--no-pager", "diff", "--no-index", "--", pathA, pathB)
	if err != nil {
		return "", false, err
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
			return string(out), true, nil
		}

		return "", false, fmt.Errorf("git diff --no-index: %w\n%s", err, out)
	}

	return string(out), false, nil
}
