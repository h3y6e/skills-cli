package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type addOptions struct {
	dir    string
	skills []string
	dryRun bool
	yes    bool
}

func newAddCmd() *cobra.Command {
	var opts addOptions

	c := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from a source repository (destructive when overwriting)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, args[0], opts)
		},
	}

	c.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show the skills that would be installed without installing")
	c.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Overwrite existing skills without confirmation")
	bindSkillFlag(c, &opts.skills, "Install only these skills")
	bindDirFlag(c, &opts.dir)

	return c
}

func runAdd(cmd *cobra.Command, source string, opts addOptions) error {
	ctx := cmd.Context()
	layout := lock.NewLayout(opts.dir)

	ref, err := skill.ParseSource(source)
	if err != nil {
		return err
	}

	tracked, err := installsTracked(layout, ref)
	if err != nil {
		return err
	}

	show := status(cmd.ErrOrStderr())
	show("fetching " + ref.CanonicalSource)
	snap, err := fetchSource(ctx, ref, tracked)

	show("")

	if err != nil {
		return err
	}

	defer func() { _ = snap.Close() }()

	discovered, err := skill.DiscoverSkills(snap.FS)
	if err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}

	discovered = skill.FilterDiscoveredSkills(discovered, opts.skills)
	if len(discovered) == 0 {
		return errors.New("no skills found to install")
	}

	overwrite := printInstallPlan(cmd.OutOrStdout(), layout, discovered)

	if opts.dryRun {
		return nil
	}

	if overwrite {
		if err := approve(cmd, opts.yes, "Overwrite existing skills?"); err != nil {
			return err
		}
	}

	if tracked {
		return skill.InstallTrackedSkills(discovered, snap, ref, layout)
	}

	return skill.InstallSkills(discovered, ref, layout)
}

// fetchSource fetches ref.Ref, or else, in gh skill format, the latest
// release, as gh skill install does, and the default branch otherwise.
func fetchSource(ctx context.Context, ref skill.SourceRef, tracked bool) (git.Snapshot, error) {
	resolve := git.ResolveDefault
	if tracked {
		resolve = git.ResolveLatest
	}

	resolved, err := resolve(ctx, ref.CloneURL, ref.Ref)
	if err != nil {
		return git.Snapshot{}, fmt.Errorf("resolve ref for %s: %w", ref.CanonicalSource, err)
	}

	snap, err := git.Fetch(ctx, ref.CloneURL, resolved)
	if err != nil {
		return git.Snapshot{}, fmt.Errorf("fetch %s: %w", ref.CanonicalSource, err)
	}

	return snap, nil
}

func installsTracked(layout lock.Layout, ref skill.SourceRef) (bool, error) {
	lockfileExists := false

	if layout.UsesLockfile() {
		_, err := lock.ReadFile(layout.LockPath())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}

		lockfileExists = err == nil
	}

	tracked := skill.PreferTrackedInstall(lockfileExists, ref.SourceType)
	if !tracked && !layout.UsesLockfile() {
		return false, fmt.Errorf("%s sources can't be added to %q: skills-lock.json records only %s", ref.SourceType, layout.Dir, lock.DefaultDir)
	}

	return tracked, nil
}

func printInstallPlan(w io.Writer, layout lock.Layout, skills []skill.DiscoveredSkill) bool {
	overwrite := false

	for _, s := range skills {
		action := "install"
		if _, err := os.Stat(layout.SkillDir(s.Name)); err == nil {
			action = "overwrite"
			overwrite = true
		}

		fmt.Fprintf(w, "%s %s\n", action, layout.SkillDir(s.Name))
	}

	return overwrite
}
