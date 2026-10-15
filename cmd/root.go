// Package cmd implements the skolar command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	cobra_usage "github.com/jdx/usage/integrations/cobra"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewUpstreamFuncs is replaceable for testing (same pattern as IsTTY).
var NewUpstreamFuncs = skill.NewUpstreamFuncs

// NewRootCmd builds the skolar command tree.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "skolar",
		Short: "Manage local agent skills",
		Long: `Manage local agent skills.

Every flag except --help and --version can also be set with a SKOLAR_<FLAG> environment variable, e.g. SKOLAR_DIR.

Fetched repositories are cached in $XDG_CACHE_HOME/skolar, or else the user cache directory: ~/Library/Caches/skolar on macOS, %LocalAppData%\skolar on Windows, ~/.cache/skolar elsewhere.

Exit status: 0 on success, 1 on failure (including a skill that could not be checked), 2 on usage errors or a missing --yes.`,
		Version:       resolveVersion(version),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return c.Help()
			}

			msg := fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath())
			if s := c.SuggestionsFor(args[0]); len(s) > 0 {
				msg += "\n\nDid you mean this?\n\t" + strings.Join(s, "\n\t")
			}

			return usageError(c, errors.New(msg))
		},
	}
	root.SetFlagErrorFunc(usageError)

	root.AddCommand(newAddCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newRemoveCmd())
	root.AddCommand(newVerifyCmd())

	return root
}

// Execute runs the CLI and returns the process exit status.
func Execute(version string) int {
	root := NewRootCmd(version)
	if slices.Contains(os.Args[1:], "--usage-spec") {
		fmt.Print(cobra_usage.Generate(root))
		return 0
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	sigs := make(chan os.Signal, 1)

	for _, s := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		// An inherited ignore (e.g. a background job) must stay, or reraise would block.
		if !signal.Ignored(s) {
			signal.Notify(sigs, s)
		}
	}

	go func() { cancel(signalError{<-sigs}) }()

	c, err := root, applyEnv(root)
	if err == nil {
		c, err = root.ExecuteContextC(ctx)
	}

	if sig, ok := errors.AsType[signalError](context.Cause(ctx)); ok {
		reraise(sig.Signal)
	}

	if err == nil {
		return 0
	}

	rec := newErrorRecord(err)
	// Handlers write their own record; usage errors stop before any handler runs.
	if key := c.Annotations[recordKey]; key != "" && rec.Code == codeUsage {
		if asJSON, _ := c.Flags().GetBool("json"); asJSON {
			_ = writeRecord(c.OutOrStdout(), key, []any{}, err)
		}
	}

	fmt.Fprintf(os.Stderr, "error: %s\n", rec.Message)

	if rec.Fix != "" {
		fmt.Fprintf(os.Stderr, "fix: %s\n", rec.Fix)
	}

	if rec.Code == codeUsage || rec.Code == codeConfirmationRequired {
		return 2
	}

	return 1
}

// recordKey annotates a command with the key of its --json result collection.
const recordKey = "skolar/record"

type signalError struct{ os.Signal }

func (e signalError) Error() string { return "received " + e.String() }

func reraise(sig os.Signal) {
	signal.Reset()

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}

	if p.Signal(sig) == nil {
		select {}
	}
}

// applyEnv makes each SKOLAR_<FLAG> the default of its flag, before parsing lets the command line override it.
// Cobra adds --help and --version during execution, so they have no variable.
func applyEnv(root *cobra.Command) error {
	commands := []*cobra.Command{root}
	for len(commands) > 0 {
		c := commands[0]
		commands = append(commands[1:], c.Commands()...)

		var err error

		c.Flags().VisitAll(func(f *pflag.Flag) {
			name := "SKOLAR_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))

			value := os.Getenv(name)
			if err != nil || value == "" {
				return
			}

			if slice, ok := f.Value.(pflag.SliceValue); ok {
				err = slice.Replace(strings.Split(value, ","))
			} else {
				err = f.Value.Set(value)
			}

			if err != nil {
				err = usageError(root, fmt.Errorf("%s: %w", name, err))
				return
			}

			f.DefValue = f.Value.String()
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func printSkippedEntries(w io.Writer, skipped []skill.SkippedEntry) {
	for _, s := range skipped {
		fmt.Fprintf(w, "warning: skipping %q (unsupported source type %q)\n", s.SkillName, s.SourceType)
	}
}

func bindSkillFlag(c *cobra.Command, target *[]string, usage string) {
	c.Flags().StringSliceVarP(target, "skill", "s", nil, usage+"; globs like 'gke-*' match names (repeatable)")
	c.PreRunE = func(c *cobra.Command, _ []string) error {
		if err := skill.CheckPatterns(*target); err != nil {
			return usageError(c, err)
		}

		return nil
	}
}

func bindDirFlag(c *cobra.Command, target *string) {
	c.Flags().StringVarP(target, "dir", "d", lock.DefaultDir, "Skill directory; .agents/skills beside a skills-lock.json uses the lockfile, others SKILL.md metadata")
}

type inventory struct {
	layout   lock.Layout
	lockFile lock.File
	entries  map[string]lock.Entry
	foreign  []skill.InstalledSkill
}

func loadInventory(dir string) (inventory, error) {
	layout := lock.NewLayout(dir)

	lf, err := layout.ReadLockFile()
	if err != nil {
		return inventory{}, err
	}

	installed, err := skill.ScanInstalledSkills(layout.Dir)
	if err != nil {
		return inventory{}, err
	}

	entries := layout.Entries(lf)

	return inventory{layout: layout, lockFile: lf, entries: entries, foreign: skill.UnmanagedSkills(installed, entries)}, nil
}

func (inv inventory) locked(name string) bool {
	_, ok := inv.entries[name]
	return ok
}

func (inv inventory) names() []string {
	names := slices.Collect(maps.Keys(inv.entries))
	for _, s := range inv.foreign {
		names = append(names, s.Name)
	}

	slices.Sort(names)

	return slices.Compact(names)
}

func (inv inventory) recorded(name string) bool {
	return inv.locked(name) || slices.ContainsFunc(inv.foreign, func(s skill.InstalledSkill) bool { return s.Name == name })
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
