package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/h3y6e/skills/internal/git"
	"github.com/h3y6e/skills/internal/lock"
	"github.com/h3y6e/skills/internal/skill"
	"github.com/spf13/cobra"
)

// Git operations as variables, replaceable for testing (same pattern as IsTTY).
var (
	ShallowClone     = git.ShallowClone
	ResolveRef       = git.ResolveRef
	NewUpstreamFuncs = skill.NewUpstreamFuncs
)

func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "skills",
		Short:         "Manage local agent skills",
		Version:       resolveVersion(version),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newAddCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newSetupGitHubActionsCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newRemoveCmd())

	return root
}

func Execute(version string) error {
	return NewRootCmd(version).Execute()
}

// loadLockFile reads the lockfile, returning an empty file when it does not
// exist (e.g. destinations managed purely in gh skill format).
func loadLockFile(layout lock.Layout) (lock.File, error) {
	lf, err := lock.ReadFile(layout.LockPath())
	if errors.Is(err, os.ErrNotExist) {
		return lock.File{Version: 1, Skills: map[string]lock.Entry{}}, nil
	}
	if err != nil {
		return lock.File{}, fmt.Errorf("read lockfile: %w", err)
	}
	return lf, nil
}

func printSkippedEntries(w io.Writer, skipped []skill.SkippedEntry) {
	for _, s := range skipped {
		fmt.Fprintf(w, "warning: skipping %q (unsupported source type %q)\n", s.SkillName, s.SourceType)
	}
}

func bindDestFlag(c *cobra.Command, target *string, description string) {
	c.Flags().StringVarP(target, "dest", "d", lock.DefaultDestDir, description)
}

func newAddCmd() *cobra.Command {
	var (
		listFlag  bool
		skillFlag []string
		destFlag  string
	)

	c := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from a source repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, args[0], listFlag, skillFlag, destFlag)
		},
	}

	c.Flags().BoolVar(&listFlag, "list", false, "List available skills without installing")
	c.Flags().StringSliceVarP(&skillFlag, "skill", "s", nil, "Install only specified skills (repeatable)")
	bindDestFlag(c, &destFlag, "Destination directory for installed skills")

	return c
}

func newCheckCmd() *cobra.Command {
	var (
		destFlag string
		jsonFlag bool
	)

	c := &cobra.Command{
		Use:   "check",
		Short: "Check installed skills for updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, destFlag, jsonFlag)
		},
	}

	bindDestFlag(c, &destFlag, "Skill install directory (lockfile is placed alongside)")
	c.Flags().BoolVar(&jsonFlag, "json", false, "Output results in JSON format")

	return c
}

func newUpdateCmd() *cobra.Command {
	var (
		skillFlag []string
		destFlag  string
		yesFlag   bool
	)

	c := &cobra.Command{
		Use:   "update [source]",
		Short: "Update installed skills from their sources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) > 0 {
				source = args[0]
			}
			return runUpdate(cmd, source, skillFlag, destFlag, yesFlag)
		},
	}

	c.Flags().StringSliceVarP(&skillFlag, "skill", "s", nil, "Update only specified skills (repeatable)")
	bindDestFlag(c, &destFlag, "Destination directory for installed skills")
	c.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Apply updates even without a TTY")

	return c
}

func newListCmd() *cobra.Command {
	var (
		destFlag string
		jsonFlag bool
	)

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, destFlag, jsonFlag)
		},
	}

	bindDestFlag(c, &destFlag, "Skill install directory (lockfile is placed alongside)")
	c.Flags().BoolVar(&jsonFlag, "json", false, "Output results in JSON format")

	return c
}

func newRemoveCmd() *cobra.Command {
	var destFlag string

	c := &cobra.Command{
		Use:     "remove <skill>...",
		Aliases: []string{"rm"},
		Short:   "Remove installed skills",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args, destFlag)
		},
	}

	bindDestFlag(c, &destFlag, "Destination directory for installed skills")

	return c
}

func resolveVersion(version string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	if version == "dev" {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			version = v
		}
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return version + " (" + s.Value[:7] + ")"
		}
	}
	return version
}
