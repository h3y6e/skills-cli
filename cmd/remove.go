package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
	"github.com/spf13/cobra"
)

func runRemove(cmd *cobra.Command, skillNames []string, destDir string) error {
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
			return fmt.Errorf("skill %q not found in %q", name, layout.DestDir)
		}
	}

	lockDirty := false
	for _, name := range skillNames {
		skillDir := filepath.Join(layout.DestDir, name)
		if err := os.RemoveAll(skillDir); err != nil {
			return fmt.Errorf("remove skill directory %q: %w", name, err)
		}

		if _, ok := entries[name]; ok {
			delete(lf.Skills, name)
			lockDirty = true
		}

		fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", name)
	}

	if lockDirty {
		if err := lock.WriteFile(layout.LockPath(), lf); err != nil {
			return fmt.Errorf("write lockfile: %w", err)
		}
	}

	return nil
}
