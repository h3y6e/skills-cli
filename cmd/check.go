package cmd

import (
	"fmt"

	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
	"github.com/spf13/cobra"
)

func runCheck(cmd *cobra.Command, destDir string, jsonFlag bool) error {
	layout := lock.NewLayout(destDir)
	lf, err := loadLockFile(layout)
	if err != nil {
		return err
	}
	entries := lock.FilterEntriesByDest(lf.Skills, layout.DestDir)

	installed, err := skill.ScanInstalledSkills(layout.DestDir)
	if err != nil {
		return err
	}
	foreign := skill.UnmanagedSkills(installed, entries)

	if len(entries) == 0 && len(foreign) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no installed skills to check")
		return nil
	}

	cloneFn, cleanup := skill.NewCloneFunc(cmd.Context(), "skills-check-*")
	defer cleanup()

	candidates, skipped, err := skill.AggregateUpdateCandidates(entries, cloneFn)
	if err != nil {
		return err
	}

	fns, foreignCleanup := NewUpstreamFuncs(cmd.Context(), "skills-check-foreign-*")
	defer foreignCleanup()

	foreignCandidates, err := skill.AggregateForeignUpdateCandidates(foreign, fns)
	if err != nil {
		return err
	}
	candidates = append(candidates, foreignCandidates...)

	printSkippedEntries(cmd.OutOrStdout(), skipped)

	outputMode := ResolveOutputMode(jsonFlag)
	renderer := NewCheckRenderer(outputMode, cmd.OutOrStdout())

	for _, c := range candidates {
		renderer.Render(NewCheckResult(c))
	}

	return renderer.Flush()
}
