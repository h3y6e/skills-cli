package cmd

import (
	"fmt"
	"os"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

func runRemove(cmd *cobra.Command, skillNames []string, destDir string, yes, dryRun bool) error {
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
	foreign := make(map[string]skill.InstalledSkill)
	for _, s := range skill.UnmanagedSkills(installed, entries) {
		foreign[s.Name] = s
	}

	for _, name := range skillNames {
		_, inLock := entries[name]
		_, isForeign := foreign[name]
		if !inLock && !isForeign {
			return &cliError{codeSkillNotFound, fmt.Errorf("skill %q not found in %q", name, layout.DestDir), "skolar list --dest " + layout.DestDir}
		}
	}

	plan := cmd.OutOrStdout()
	for _, name := range skillNames {
		fmt.Fprintf(plan, "remove %s\n", layout.SkillDir(name))
	}
	if dryRun {
		return nil
	}
	if err := approve(cmd, yes, "Remove these skills?"); err != nil {
		return err
	}

	lockDirty := false
	for _, name := range skillNames {
		if err := os.RemoveAll(layout.SkillDir(name)); err != nil {
			return fmt.Errorf("remove skill directory %q: %w", name, err)
		}

		if _, ok := entries[name]; ok {
			delete(lf.Skills, name)
			lockDirty = true
		}

	}

	if lockDirty {
		if err := lock.WriteFile(layout.LockPath(), lf); err != nil {
			return fmt.Errorf("write lockfile: %w", err)
		}
	}

	return nil
}
