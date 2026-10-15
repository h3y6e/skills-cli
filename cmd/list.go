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
	SkillName    string          `json:"skillName"`
	Source       string          `json:"source"`
	Ref          string          `json:"ref,omitempty"`
	SourceType   lock.SourceType `json:"sourceType"`
	ComputedHash string          `json:"computedHash"`
}

type listOptions struct {
	dir  string
	json bool
}

func newListCmd() *cobra.Command {
	var opts listOptions

	c := &cobra.Command{
		Use:         "list",
		Aliases:     []string{"ls"},
		Annotations: map[string]string{recordKey: "skills"},
		Short:       "List installed skills",
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, opts)
		},
	}

	bindDirFlag(c, &opts.dir)
	c.Flags().BoolVar(&opts.json, "json", false, "Output results in JSON format")

	return c
}

func runList(cmd *cobra.Command, opts listOptions) (err error) {
	payload := []listEntryJSON{}
	if opts.json {
		defer func() { err = writeRecord(cmd.OutOrStdout(), cmd.Annotations[recordKey], payload, err) }()
	}

	inv, err := loadInventory(opts.dir)
	if err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(inv.entries)) {
		entry := inv.entries[name]
		payload = append(payload, listEntryJSON{
			SkillName:    name,
			Source:       entry.Source,
			Ref:          entry.Ref,
			SourceType:   entry.SourceType,
			ComputedHash: entry.ComputedHash,
		})
	}
	for _, s := range inv.foreign {
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

	if opts.json {
		return nil
	}

	if len(payload) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no installed skills")
		return nil
	}

	for _, entry := range payload {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", entry.SkillName, skill.FormatSourceInput(entry.Source, entry.Ref), shortHash(entry.ComputedHash))
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

func foreignSourceType(meta skill.SourceMetadata) lock.SourceType {
	if meta.IsGitHub() {
		return lock.SourceGitHub
	}
	return lock.SourceLocal
}
