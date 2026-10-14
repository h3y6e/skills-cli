package cmd

import (
	"fmt"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

func runCheck(cmd *cobra.Command, skillsDir string, jsonFlag bool) (err error) {
	results := []CheckResult{}
	if jsonFlag {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], results, err) }()
	}

	layout := lock.NewLayout(skillsDir)
	lf, err := loadLockFile(layout)
	if err != nil {
		return err
	}
	entries := layout.Entries(lf)

	installed, err := skill.ScanInstalledSkills(layout.Dir)
	if err != nil {
		return err
	}
	foreign := skill.UnmanagedSkills(installed, entries)

	if len(entries) == 0 && len(foreign) == 0 {
		if !jsonFlag {
			fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills to check")
		}
		return nil
	}

	cloneFn, cleanup := skill.NewCloneFunc(cmd.Context(), "skolar-check-*")
	defer cleanup()

	candidates, skipped, err := skill.AggregateUpdateCandidates(entries, cloneFn)
	if err != nil {
		return err
	}

	fns, foreignCleanup := NewUpstreamFuncs(cmd.Context(), "skolar-check-foreign-*")
	defer foreignCleanup()

	foreignCandidates, err := skill.AggregateForeignUpdateCandidates(foreign, fns)
	if err != nil {
		return err
	}
	candidates = append(candidates, foreignCandidates...)

	printSkippedEntries(cmd.ErrOrStderr(), skipped)

	var failed []string
	for _, c := range candidates {
		result := NewCheckResult(c)
		results = append(results, result)
		if !jsonFlag {
			renderCheckResult(cmd.OutOrStdout(), result)
		}
		if c.Status == skill.StatusCheckFailed {
			failed = append(failed, c.SkillName)
		}
	}
	return checkFailed(failed)
}
