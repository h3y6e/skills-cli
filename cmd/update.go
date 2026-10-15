package cmd

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type updateOptions struct {
	dir    string
	skills []string
	dryRun bool
	yes    bool
	json   bool
}

func newUpdateCmd() *cobra.Command {
	var opts updateOptions

	c := &cobra.Command{
		Use:   "update [source]",
		Short: "Update installed skills from their sources (destructive)",
		Long: `Update installed skills from their sources (destructive).

Lists the changed skills of each source, then overwrites local changes after confirmation:
  M  changed upstream, with the lines and files update changes
  R  moved upstream; update follows it
  D  removed upstream; update keeps it, skolar remove deletes it
  A  among the source's default skills but not installed; skolar add -s installs it
  !  could not be checked
Up-to-date, pinned, and local skills are counted at the end.`,
		Annotations: map[string]string{recordKey: "results"},
		Args:        usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) > 0 {
				source = args[0]
			}

			return runUpdate(cmd, source, opts)
		},
	}

	bindSkillFlag(c, &opts.skills, "Update only these skills")
	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Apply updates without confirmation")
	c.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show the changes without applying")
	c.Flags().BoolVar(&opts.json, "json", false, "Output results in JSON format")

	return c
}

func runUpdate(cmd *cobra.Command, source string, opts updateOptions) (err error) {
	results := []updateResult{}

	if opts.json {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], results, err) }()
	}

	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}

	candidates, cleanup, err := updateCandidates(cmd, inv, source, opts.skills)
	defer cleanup()

	if err != nil || len(candidates) == 0 {
		return err
	}

	results = planUpdates(inv.layout, candidates)
	if !opts.json {
		renderPlan(cmd.OutOrStdout(), results)
		printHints(cmd.ErrOrStderr(), results, inv.layout.Dir)
	}

	apply, failed := partitionPlan(candidates)
	if opts.dryRun || len(apply) == 0 {
		return checkFailed(names(candidates, failed))
	}

	if err := approve(cmd, opts.yes, "Apply these updates?"); err != nil {
		return err
	}

	if err := applyUpdates(inv, candidates, apply, results); err != nil {
		return err
	}

	if !opts.json {
		fmt.Fprintln(cmd.OutOrStdout(), appliedSummary(results))
	}

	return checkFailed(names(candidates, failed))
}

func occupied(layout lock.Layout, name string) bool {
	_, err := os.Lstat(layout.SkillDir(name))

	return err == nil
}

// updateCandidates resolves the installed skills update covers, and the
// upstream skills not installed anywhere in the directory.
func updateCandidates(cmd *cobra.Command, inv inventory, source string, skills []string) ([]skill.UpdateCandidate, func(), error) {
	entries, err := filterEntriesBySource(inv.entries, source)
	if err != nil {
		return nil, func() {}, err
	}

	foreign, err := skill.FilterInstalledBySource(inv.foreign, source)
	if err != nil {
		return nil, func() {}, err
	}

	if len(skills) > 0 {
		entries = maps.Clone(entries)
		maps.DeleteFunc(entries, func(name string, _ lock.Entry) bool { return !skill.Selected(skills, name) })
		foreign = slices.DeleteFunc(slices.Clone(foreign), func(s skill.InstalledSkill) bool { return !skill.Selected(skills, s.Name) })
	}

	stderr := cmd.ErrOrStderr()
	if len(entries) == 0 && len(foreign) == 0 {
		fmt.Fprintln(stderr, "no installed skills to update")
		return nil, func() {}, nil
	}

	candidates, skipped, cleanup := collectCandidates(cmd.Context(), stderr, entries, foreign)
	printSkippedEntries(stderr, skipped)

	candidates = slices.DeleteFunc(skill.FilterCandidates(candidates, skills), func(c skill.UpdateCandidate) bool {
		return c.Status == skill.StatusNew && occupied(inv.layout, c.SkillName)
	})
	if len(candidates) == 0 {
		fmt.Fprintln(stderr, "no matching skills to update")
	}

	return candidates, cleanup, nil
}

func collectCandidates(
	ctx context.Context, stderr io.Writer, entries map[string]lock.Entry, foreign []skill.InstalledSkill,
) ([]skill.UpdateCandidate, []skill.SkippedEntry, func()) {
	cloneFn, cloneCleanup := skill.NewCloneFunc(ctx)
	fns, upstreamCleanup := NewUpstreamFuncs(ctx)

	candidates, skipped := skill.CollectUpdateCandidates(entries, foreign, cloneFn, fns, progress(stderr))

	return candidates, skipped, func() { cloneCleanup(); upstreamCleanup() }
}

// progress counts fetched sources on a terminal, erasing the count when done.
func progress(w io.Writer) func(n, total int) {
	show := status(w)

	return func(n, total int) {
		if n == total {
			show("")
			return
		}

		show(fmt.Sprintf("checking %d/%d sources", n, total))
	}
}

// planUpdates describes each candidate, counting what applying it changes.
// A candidate whose changes cannot be counted fails its check.
func planUpdates(layout lock.Layout, candidates []skill.UpdateCandidate) []updateResult {
	results := make([]updateResult, len(candidates))
	for i, c := range candidates {
		var diff *diffStat

		if c.Applicable() {
			stat, err := skill.DiffStat(c, layout.SkillDir(c.SkillName))
			if err != nil {
				candidates[i].Status, candidates[i].Reason = skill.StatusCheckFailed, fmt.Sprintf("count changes: %v", err)
			} else {
				diff = &diffStat{stat.Added, stat.Deleted, stat.Files}
			}
		}

		results[i] = newUpdateResult(candidates[i])
		results[i].Diff = diff
	}

	return results
}

func partitionPlan(candidates []skill.UpdateCandidate) (apply, failed []int) {
	for i, c := range candidates {
		switch {
		case c.Status == skill.StatusCheckFailed:
			failed = append(failed, i)
		case c.Applicable():
			apply = append(apply, i)
		}
	}

	return apply, failed
}

func applyUpdates(inv inventory, candidates []skill.UpdateCandidate, apply []int, results []updateResult) error {
	var locked []skill.UpdateCandidate

	for _, i := range apply {
		if !candidates[i].Foreign {
			locked = append(locked, candidates[i])
		}
	}

	if len(locked) > 0 {
		if err := skill.ApplyCandidateUpdates(inv.lockFile, locked, inv.layout); err != nil {
			return err
		}
	}

	for _, i := range apply {
		results[i].Applied = !candidates[i].Foreign
	}

	for _, i := range apply {
		c := candidates[i]
		if !c.Foreign {
			continue
		}

		if err := skill.ReplaceSkill(c.SkillName, c.Upstream, inv.layout); err != nil {
			return fmt.Errorf("update %s: %w", c.SkillName, err)
		}

		results[i].Applied = true
	}

	return nil
}

func names(candidates []skill.UpdateCandidate, indexes []int) []string {
	out := make([]string, len(indexes))
	for j, i := range indexes {
		out[j] = candidates[i].SkillName
	}

	return out
}

var statusMarks = map[skill.Status]string{
	skill.StatusUpdateAvailable: "M",
	skill.StatusMoved:           "R",
	skill.StatusRemoved:         "D",
	skill.StatusCheckFailed:     "!",
}

func renderPlan(w io.Writer, results []updateResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	bySource := map[string][]updateResult{}
	counts := map[skill.Status]int{}

	for _, r := range results {
		if _, ok := statusMarks[r.Status]; ok || r.Status == skill.StatusNew {
			bySource[r.Source] = append(bySource[r.Source], r)
		} else {
			counts[r.Status]++
		}
	}

	for _, source := range slices.Sorted(maps.Keys(bySource)) {
		fmt.Fprintln(tw, source)

		var news []string

		for _, r := range bySource[source] {
			if r.Status == skill.StatusNew {
				news = append(news, r.SkillName)
				continue
			}

			fmt.Fprintf(tw, "  %s\t%s\t%s\n", statusMarks[r.Status], r.SkillName, describe(r))
		}

		if len(news) > 0 {
			fmt.Fprintf(tw, "  A\t%d not installed: %s\n", len(news), strings.Join(news, ", "))
		}
	}

	_ = tw.Flush()

	var tally []string

	for _, s := range []skill.Status{skill.StatusUpToDate, skill.StatusPinned, skill.StatusLocal} {
		if counts[s] > 0 {
			tally = append(tally, fmt.Sprintf("%d %s", counts[s], s))
		}
	}

	if len(tally) > 0 {
		fmt.Fprintln(w, strings.Join(tally, ", "))
	}
}

func describe(r updateResult) string {
	switch r.Status {
	case skill.StatusMoved:
		moved := r.PreviousPath + " -> " + r.Path
		if r.Diff != nil && r.Diff.Files > 0 {
			moved += "  " + r.Diff.String()
		}

		return moved
	case skill.StatusRemoved:
		return "removed upstream"
	case skill.StatusCheckFailed:
		return strings.Join(strings.Fields(r.Reason), " ")
	case skill.StatusUpdateAvailable:
		return r.Diff.String()
	case skill.StatusUpToDate, skill.StatusNew, skill.StatusPinned, skill.StatusLocal:
	}

	return ""
}

func printHints(w io.Writer, results []updateResult, dir string) {
	var removed []string

	reinstall := ""

	for _, r := range results {
		if r.Status == skill.StatusRemoved {
			removed = append(removed, r.SkillName)
		}

		if r.Status == skill.StatusNew {
			reinstall = cmp.Or(reinstall, r.reinstall)
		}
	}

	if len(removed) > 0 {
		fmt.Fprintf(w, "hint: skolar remove %s --dir %s deletes the skills removed upstream\n", strings.Join(removed, " "), dir)
	}

	if reinstall != "" {
		fmt.Fprintf(w, "hint: skolar add %s -s <skill> --dir %s installs a skill not installed\n", reinstall, dir)
	}
}

func appliedSummary(results []updateResult) string {
	counts := map[skill.Status]int{}

	for _, r := range results {
		if r.Applied {
			counts[r.Status]++
		}
	}

	var parts []string
	if n := counts[skill.StatusUpdateAvailable]; n > 0 {
		parts = append(parts, fmt.Sprintf("updated %d", n))
	}

	if n := counts[skill.StatusMoved]; n > 0 {
		parts = append(parts, fmt.Sprintf("moved %d", n))
	}

	return strings.Join(parts, ", ")
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
