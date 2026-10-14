package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

func runAdd(cmd *cobra.Command, source string, dryRun, yes bool, skillFilter []string, skillsDir string) error {
	ctx := cmd.Context()
	layout := lock.NewLayout(skillsDir)

	ref, err := skill.ParseSource(source)
	if err != nil {
		return err
	}

	_, err = lock.ReadFile(layout.LockPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lockfileForDir := err == nil && layout.UsesLockfile()
	tracked := skill.PreferTrackedInstall(lockfileForDir, ref.SourceType)

	// gh skill format resolves the ref like gh skill install: explicit ref,
	// else latest release, else default branch.
	cloneRef := ref.Ref
	var resolved git.ResolvedRef
	if tracked {
		resolved, err = ResolveRef(ctx, ref.CloneURL, ref.Ref)
		if err != nil {
			return fmt.Errorf("resolve ref for %s: %w", ref.CanonicalSource, err)
		}
		cloneRef = resolved.Full
	}

	tmpDir, err := os.MkdirTemp("", "skolar-add-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := ShallowClone(ctx, ref.CloneURL, cloneRef, tmpDir); err != nil {
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

	overwrite := false
	for _, s := range discovered {
		action := "install"
		if _, err := os.Stat(layout.SkillDir(s.Name)); err == nil {
			action = "overwrite"
			overwrite = true
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", action, layout.SkillDir(s.Name))
	}
	if dryRun {
		return nil
	}
	if overwrite {
		if err := approve(cmd, yes, "Overwrite existing skills?"); err != nil {
			return err
		}
	}

	if tracked {
		return skill.InstallTrackedSkills(ctx, discovered, tmpDir, ref, resolved, layout)
	}
	return skill.InstallSkills(discovered, ref, layout)
}
