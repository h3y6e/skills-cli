package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
	"github.com/spf13/cobra"
)

func runAdd(cmd *cobra.Command, source string, listMode bool, skillFilter []string, destDir string) error {
	layout := lock.NewLayout(destDir)

	ref, err := skill.ParseSource(source)
	if err != nil {
		return err
	}

	// Mode selection: a lockfile alongside the destination keeps the vercel
	// format; otherwise install in gh skill format (frontmatter metadata).
	_, statErr := os.Stat(layout.LockPath())
	lockfileExists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat lockfile: %w", statErr)
	}
	if !listMode && skill.PreferTrackedInstall(lockfileExists, ref.SourceType) {
		return runAddTracked(cmd, ref, skillFilter, layout)
	}

	tmpDir, err := os.MkdirTemp("", "skills-add-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ShallowClone(cmd.Context(), ref.CloneURL, ref.Ref, tmpDir); err != nil {
		return fmt.Errorf("clone %s: %w", ref.CanonicalSource, err)
	}

	discovered, err := skill.DiscoverSkills(tmpDir)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	discovered = skill.FilterDiscoveredSkills(discovered, skillFilter)

	if listMode {
		for _, s := range discovered {
			fmt.Fprintln(cmd.OutOrStdout(), s.Name)
		}
		return nil
	}

	if len(discovered) == 0 {
		return fmt.Errorf("no skills found to install")
	}

	return skill.InstallSkills(discovered, ref, layout)
}

// runAddTracked installs skills in gh skill format: the ref is resolved like
// gh skill install (explicit ref, else latest release, else default branch),
// source metadata is injected into each SKILL.md, and no lockfile is written.
func runAddTracked(cmd *cobra.Command, ref skill.SourceRef, skillFilter []string, layout lock.Layout) error {
	ctx := cmd.Context()

	resolved, err := ResolveRef(ctx, ref.CloneURL, ref.Ref)
	if err != nil {
		return fmt.Errorf("resolve ref for %s: %w", ref.CanonicalSource, err)
	}

	tmpDir, err := os.MkdirTemp("", "skills-add-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ShallowClone(ctx, ref.CloneURL, resolved.Full, tmpDir); err != nil {
		return fmt.Errorf("clone %s: %w", ref.CanonicalSource, err)
	}

	discovered, err := skill.DiscoverSkills(tmpDir)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	discovered = skill.FilterDiscoveredSkills(discovered, skillFilter)
	if len(discovered) == 0 {
		return fmt.Errorf("no skills found to install")
	}

	if err := skill.InstallTrackedSkills(ctx, discovered, tmpDir, ref, resolved, layout); err != nil {
		return err
	}

	for _, s := range discovered {
		fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", s.Name)
	}
	return nil
}
