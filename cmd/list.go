package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
	"github.com/spf13/cobra"
)

type listEntryJSON struct {
	SkillName    string `json:"skillName"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	ComputedHash string `json:"computedHash"`
}

func runList(cmd *cobra.Command, destDir string, jsonFlag bool) error {
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

	payload := make([]listEntryJSON, 0, len(entries)+len(foreign))
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[name]
		payload = append(payload, listEntryJSON{
			SkillName:    name,
			Source:       entry.Source,
			Ref:          entry.Ref,
			SourceType:   entry.SourceType,
			ComputedHash: entry.ComputedHash,
		})
	}
	for _, s := range foreign {
		payload = append(payload, listEntryJSON{
			SkillName:    s.Name,
			Source:       foreignSource(s.Meta),
			Ref:          s.Meta.Ref,
			SourceType:   foreignSourceType(s.Meta),
			ComputedHash: s.Meta.TreeSHA,
		})
	}
	slices.SortFunc(payload, func(a, b listEntryJSON) int {
		return cmp.Compare(a.SkillName, b.SkillName)
	})

	if jsonFlag {
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}

	if len(payload) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no installed skills")
		return nil
	}

	for _, entry := range payload {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", entry.SkillName, skill.FormatSourceInput(entry.Source, entry.Ref), ShortHash(entry.ComputedHash))
	}
	return nil
}

func foreignSource(meta skill.SourceMetadata) string {
	if meta.IsGitHub() {
		if source, err := skill.OwnerRepoFromURL(meta.RepoURL); err == nil {
			return source
		}
		return meta.RepoURL
	}
	return meta.LocalPath
}

func foreignSourceType(meta skill.SourceMetadata) string {
	if meta.IsGitHub() {
		return "github"
	}
	return "local"
}
