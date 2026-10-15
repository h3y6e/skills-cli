package cmd

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type verifyOptions struct {
	dir    string
	skills []string
	json   bool
}

func newVerifyCmd() *cobra.Command {
	var opts verifyOptions

	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify installed skills against their records",
		Long: `Verify installed skills against their records.

Lockfile skills are hashed on disk and compared with computedHash, offline; on a mismatch the source is cloned to compare file by file.
gh skill metadata is compared file by file with the tree github-tree-sha names, fetched at github-ref.
Exits 1 when a skill is modified or cannot be verified.`,
		Annotations: map[string]string{recordKey: "results"},
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd, opts)
		},
	}

	c.Flags().StringSliceVarP(&opts.skills, "skill", "s", nil, "Verify only specified skills (repeatable)")
	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVar(&opts.json, "json", false, "Output results in JSON format")

	return c
}

func runVerify(cmd *cobra.Command, opts verifyOptions) (err error) {
	results := []skill.VerifyResult{}
	if opts.json {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], results, err) }()
	}

	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}
	layout, entries, foreign := inv.layout, maps.Clone(inv.entries), inv.foreign
	unrecorded, err := skill.VerifyUnrecorded(layout.Dir, inv.recorded)
	if err != nil {
		return err
	}
	if skillFilter := opts.skills; len(skillFilter) > 0 {
		maps.DeleteFunc(entries, func(name string, _ lock.Entry) bool { return !slices.Contains(skillFilter, name) })
		foreign = slices.DeleteFunc(foreign, func(s skill.InstalledSkill) bool { return !slices.Contains(skillFilter, s.Name) })
		unrecorded = slices.DeleteFunc(unrecorded, func(r skill.VerifyResult) bool { return !slices.Contains(skillFilter, r.SkillName) })
	}

	fns, cleanup := NewUpstreamFuncs(cmd.Context(), "skolar-verify-*")
	defer cleanup()
	cloneFn, cloneCleanup := skill.NewCloneFunc(cmd.Context(), "skolar-verify-lock-*")
	defer cloneCleanup()

	all := slices.Concat(skill.VerifyLocked(layout, entries, cloneFn), skill.VerifyTracked(foreign, fns), unrecorded)
	slices.SortFunc(all, func(a, b skill.VerifyResult) int { return cmp.Compare(a.SkillName, b.SkillName) })
	var failed []skill.VerifyResult
	for _, r := range all {
		results = append(results, r)
		if !opts.json {
			renderVerifyResult(cmd.OutOrStdout(), r)
		}
		if r.Status == skill.VerifyModified || r.Status == skill.VerifyUnverifiable {
			failed = append(failed, r)
		}
	}
	if len(results) == 0 && !opts.json {
		fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills to verify")
	}
	return verifyFailed(failed, layout.Dir)
}

func renderVerifyResult(w io.Writer, r skill.VerifyResult) {
	switch {
	case len(r.Files) > 0:
		fmt.Fprintf(w, "%s: %s (%s)\n", r.SkillName, r.Status, strings.Join(r.Files, ", "))
	case r.Reason != "":
		fmt.Fprintf(w, "%s: %s (%s)\n", r.SkillName, r.Status, r.Reason)
	default:
		fmt.Fprintf(w, "%s: %s\n", r.SkillName, r.Status)
	}
}

func verifyFailed(failed []skill.VerifyResult, dir string) error {
	if len(failed) == 0 {
		return nil
	}
	names := make([]string, len(failed))
	for i, r := range failed {
		names[i] = r.SkillName
	}
	var fix string
	if i := slices.IndexFunc(failed, func(r skill.VerifyResult) bool { return r.Reinstall != "" }); i >= 0 {
		fix = fmt.Sprintf("skolar add %s -s %s --dir %s -y", failed[i].Reinstall, failed[i].SkillName, dir)
	}
	return &cliError{
		code: codeVerifyFailed,
		err:  fmt.Errorf("%s did not verify", strings.Join(names, ", ")),
		fix:  fix,
	}
}
