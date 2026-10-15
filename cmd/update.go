package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type updateOptions struct {
	dir    string
	skills []string
	dryRun bool
	yes    bool
}

func newUpdateCmd() *cobra.Command {
	var opts updateOptions

	c := &cobra.Command{
		Use:   "update [source]",
		Short: "Update installed skills from their sources (destructive)",
		Long: `Update installed skills from their sources (destructive).

Shows the diff of each skill, then overwrites local changes after confirmation.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) > 0 {
				source = args[0]
			}

			return runUpdate(cmd, source, opts)
		},
	}

	c.Flags().StringSliceVarP(&opts.skills, "skill", "s", nil, "Update only specified skills (repeatable)")
	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Apply updates without confirmation")
	c.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show the diffs without applying")

	return c
}

func runUpdate(cmd *cobra.Command, source string, opts updateOptions) error {
	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}

	entries, err := filterEntriesBySource(inv.entries, source)
	if err != nil {
		return err
	}

	foreign, err := skill.FilterInstalledBySource(inv.foreign, source)
	if err != nil {
		return err
	}

	candidates, skipped, cleanup, err := collectCandidates(cmd.Context(), "skolar-update", entries, foreign)
	defer cleanup()

	if err != nil {
		return err
	}

	candidates = skill.FilterCandidates(candidates, opts.skills)

	if len(candidates) == 0 && len(skipped) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills to update")
		return nil
	}

	printSkippedEntries(cmd.ErrOrStderr(), skipped)

	if len(candidates) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no matching skills to update")
		return nil
	}

	w := cmd.OutOrStdout()

	locked, tracked, failed := reviewUpdates(cmd.Context(), w, inv.layout, candidates)
	if opts.dryRun || len(locked)+len(tracked) == 0 {
		return checkFailed(failed)
	}

	if err := approve(cmd, opts.yes, "Apply these updates?"); err != nil {
		return err
	}

	if err := applyUpdates(w, inv, locked, tracked); err != nil {
		return err
	}

	return checkFailed(failed)
}

func applyUpdates(w io.Writer, inv inventory, locked, tracked []skill.UpdateCandidate) error {
	if len(locked) > 0 {
		if err := skill.ApplyCandidateUpdates(inv.lockFile, locked, inv.layout); err != nil {
			return err
		}

		for _, c := range locked {
			fmt.Fprintf(w, "%s: updated\n", c.SkillName)
		}
	}

	for _, c := range tracked {
		if err := skill.ReplaceSkill(c.SkillName, c.StagedDir, inv.layout); err != nil {
			return fmt.Errorf("update %s: %w", c.SkillName, err)
		}

		fmt.Fprintf(w, "%s: updated\n", c.SkillName)
	}

	return nil
}

func reviewUpdates(
	ctx context.Context, w io.Writer, layout lock.Layout, candidates []skill.UpdateCandidate,
) (locked, tracked []skill.UpdateCandidate, failed []string) {
	for _, c := range candidates {
		switch c.Status {
		case skill.StatusUpToDate:
			fmt.Fprintf(w, "%s: %s\n", c.SkillName, c.Status)
		case skill.StatusUpdateAvailable:
			diff, hasDiff, err := git.DiffNoIndex(ctx, layout.SkillDir(c.SkillName), c.StagedDir)
			if err != nil {
				fmt.Fprintf(w, "%s: diff error: %v\n", c.SkillName, err)
				failed = append(failed, c.SkillName)

				continue
			}

			if hasDiff {
				fmt.Fprintf(w, "--- %s ---\n%s\n", c.SkillName, diff)
			}

			if c.Foreign {
				tracked = append(tracked, c)
			} else {
				locked = append(locked, c)
			}
		case skill.StatusCheckFailed:
			fmt.Fprintf(w, "%s: %s (%s)\n", c.SkillName, c.Status, c.Reason)
			failed = append(failed, c.SkillName)
		case skill.StatusPinned:
			fmt.Fprintf(w, "%s: pinned to %s (skipped)\n", c.SkillName, c.Reason)
		case skill.StatusLocal:
			fmt.Fprintf(w, "%s: installed from local path (skipped)\n", c.SkillName)
		}
	}

	return locked, tracked, failed
}

func filterEntriesBySource(entries map[string]lock.Entry, source string) (map[string]lock.Entry, error) {
	if source == "" {
		return entries, nil
	}

	ref, err := skill.ParseSource(source)
	if err != nil {
		return nil, err
	}

	filtered := make(map[string]lock.Entry)

	for name, entry := range entries {
		if entry.Source != ref.CanonicalSource {
			continue
		}

		if ref.Ref != "" && entry.Ref != ref.Ref {
			continue
		}

		filtered[name] = entry
	}

	return filtered, nil
}
