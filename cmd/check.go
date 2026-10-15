package cmd

import (
	"fmt"

	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type checkOptions struct {
	dir  string
	json bool
}

func newCheckCmd() *cobra.Command {
	var opts checkOptions

	c := &cobra.Command{
		Use:         "check",
		Short:       "Check installed skills for updates",
		Annotations: map[string]string{recordKey: "results"},
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheck(cmd, opts)
		},
	}

	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVar(&opts.json, "json", false, "Output results in JSON format")

	return c
}

func runCheck(cmd *cobra.Command, opts checkOptions) (err error) {
	results := []checkResult{}

	if opts.json {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], results, err) }()
	}

	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}

	if len(inv.entries) == 0 && len(inv.foreign) == 0 {
		if !opts.json {
			fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills to check")
		}

		return nil
	}

	candidates, skipped, cleanup, err := collectCandidates(cmd.Context(), "skolar-check", inv.entries, inv.foreign)
	defer cleanup()

	if err != nil {
		return err
	}

	printSkippedEntries(cmd.ErrOrStderr(), skipped)

	var failed []string

	for _, c := range candidates {
		result := newCheckResult(c)

		results = append(results, result)
		if !opts.json {
			renderCheckResult(cmd.OutOrStdout(), result)
		}

		if c.Status == skill.StatusCheckFailed {
			failed = append(failed, c.SkillName)
		}
	}

	return checkFailed(failed)
}
