package cmd

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	"github.com/spf13/cobra"
)

type listEntryJSON struct {
	SkillName    string `json:"skillName"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	ComputedHash string `json:"computedHash"`
}

func runList(cmd *cobra.Command, destDir string, jsonFlag bool) (err error) {
	payload := []listEntryJSON{}
	if jsonFlag {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], payload, err) }()
	}

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
		return nil
	}

	if len(payload) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills")
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
