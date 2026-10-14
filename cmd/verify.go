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

	foreign := skill.UnmanagedSkills(installed, entries)
	recorded := func(name string) bool {
		_, inLock := entries[name]
		return inLock || slices.ContainsFunc(foreign, func(s skill.InstalledSkill) bool { return s.Name == name })
	}
	unrecorded, err := skill.VerifyUnrecorded(layout.Dir, recorded)
	if err != nil {
		return err
	}
	if len(skillFilter) > 0 {
		maps.DeleteFunc(entries, func(name string, _ lock.Entry) bool { return !slices.Contains(skillFilter, name) })
		foreign = slices.DeleteFunc(foreign, func(s skill.InstalledSkill) bool { return !slices.Contains(skillFilter, s.Name) })
		unrecorded = slices.DeleteFunc(unrecorded, func(r skill.VerifyResult) bool { return !slices.Contains(skillFilter, r.SkillName) })
	}

	fns, cleanup := NewUpstreamFuncs(cmd.Context(), "skolar-verify-*")
	defer cleanup()

	all := slices.Concat(skill.VerifyLocked(layout, entries), skill.VerifyTracked(foreign, fns), unrecorded)
	slices.SortFunc(all, func(a, b skill.VerifyResult) int { return cmp.Compare(a.SkillName, b.SkillName) })
	var failed []skill.VerifyResult
	for _, r := range all {
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
