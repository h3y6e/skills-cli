package cmd

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

func runVerify(cmd *cobra.Command, skillsDir string, skillFilter []string, jsonFlag bool) (err error) {
	results := []skill.VerifyResult{}
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

	fns, cleanup := NewUpstreamFuncs(cmd.Context(), "skolar-verify-*")
	defer cleanup()

	all := append(skill.VerifyLocked(layout, entries), skill.VerifyTracked(skill.UnmanagedSkills(installed, entries), fns)...)
	var failed []skill.VerifyResult
	for _, r := range all {
		if len(skillFilter) > 0 && !slices.Contains(skillFilter, r.SkillName) {
			continue
		}
		results = append(results, r)
		if !jsonFlag {
			renderVerifyResult(cmd.OutOrStdout(), r)
		}
		if r.Status == skill.VerifyModified || r.Status == skill.VerifyUnverifiable {
			failed = append(failed, r)
		}
	}
	if len(results) == 0 && !jsonFlag {
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
	first := failed[0]
	return &cliError{
		code: codeVerifyFailed,
		err:  fmt.Errorf("%s did not verify", strings.Join(names, ", ")),
		fix:  fmt.Sprintf("skolar add %s -s %s --dir %s -y", first.Source, first.SkillName, dir),
	}
}
