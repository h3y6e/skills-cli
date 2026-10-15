package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/spf13/cobra"
)

type removeOptions struct {
	dir    string
	dryRun bool
	yes    bool
}

func newRemoveCmd() *cobra.Command {
	var opts removeOptions

	c := &cobra.Command{
		Use:     "remove <skill>...",
		Aliases: []string{"rm"},
		Short:   "Remove installed skills (destructive)",
		Args:    usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args, opts)
		},
	}

	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Remove without confirmation")
	c.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show the directories that would be removed")

	return c
}

func runRemove(cmd *cobra.Command, skillNames []string, opts removeOptions) error {
	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}

	for _, name := range skillNames {
		if !inv.recorded(name) {
			return &cliError{
				code: codeSkillNotFound,
				err:  fmt.Errorf("skill %q not found in %q", name, inv.layout.Dir),
				fix:  "skolar list --dir " + inv.layout.Dir,
			}
		}
	}

	for _, name := range skillNames {
		fmt.Fprintf(cmd.OutOrStdout(), "remove %s\n", inv.layout.SkillDir(name))
	}
	if opts.dryRun {
		return nil
	}
	if err := approve(cmd, opts.yes, "Remove these skills?"); err != nil {
		return err
	}

	for _, name := range skillNames {
		if err := os.RemoveAll(inv.layout.SkillDir(name)); err != nil {
			return fmt.Errorf("remove skill directory %q: %w", name, err)
		}
	}

	if !slices.ContainsFunc(skillNames, inv.locked) {
		return nil
	}
	for _, name := range skillNames {
		delete(inv.lockFile.Skills, name)
	}
	if err := lock.WriteFile(inv.layout.LockPath(), inv.lockFile); err != nil {
		return fmt.Errorf("write lockfile: %w", err)
	}
	return nil
}
