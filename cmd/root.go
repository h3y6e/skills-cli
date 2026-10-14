package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	"github.com/h3y6e/skolar/internal/git"
	"github.com/h3y6e/skolar/internal/lock"
	"github.com/h3y6e/skolar/internal/skill"
	cobra_usage "github.com/jdx/usage/integrations/cobra"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Git operations as variables, replaceable for testing (same pattern as IsTTY).
var (
	ShallowClone     = git.ShallowClone
	ResolveRef       = git.ResolveRef
	NewUpstreamFuncs = skill.NewUpstreamFuncs
)

func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "skolar",
		Short: "Manage local agent skills",
		Long: `Manage local agent skills.

Every flag except --help and --version can also be set with a SKOLAR_<FLAG> environment variable, e.g. SKOLAR_DIR.

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
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usageError(c, err)
	})

	root.AddCommand(newAddCmd())
	root.AddCommand(newCheckCmd())
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
			writeRecord(c.OutOrStdout(), key, []any{}, err)
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

// Error codes are part of the --json output; renaming one is breaking.
const (
	codeFailed               = "failed"
	codeUsage                = "usage"
	codeConfirmationRequired = "confirmation_required"
	codeDeclined             = "declined"
	codeSkillNotFound        = "skill_not_found"
	codeCheckFailed          = "check_failed"
	codeVerifyFailed         = "verify_failed"
)

type cliError struct {
	code string
	err  error
	fix  string
}

func (e *cliError) Error() string { return e.err.Error() }
func (e *cliError) Unwrap() error { return e.err }

func usageError(c *cobra.Command, err error) error {
	return &cliError{codeUsage, err, c.CommandPath() + " --help"}
}

func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := validate(c, args); err != nil {
			return usageError(c, err)
		}
		return nil
	}
}

func checkFailed(names []string) error {
	if len(names) == 0 {
		return nil
	}
	return &cliError{code: codeCheckFailed, err: fmt.Errorf("could not check %s", strings.Join(names, ", "))}
}

type errorRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

func newErrorRecord(err error) errorRecord {
	rec := errorRecord{Code: codeFailed, Message: err.Error()}
	if ce, ok := errors.AsType[*cliError](err); ok {
		rec.Code, rec.Fix = ce.code, ce.fix
	}
	return rec
}

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

// loadLockFile reads the lockfile, returning an empty file when it does not
// exist (e.g. directories managed purely in gh skill format).
func loadLockFile(layout lock.Layout) (lock.File, error) {
	if !layout.UsesLockfile() {
		return lock.File{Version: 1, Skills: map[string]lock.Entry{}}, nil
	}
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

func bindDirFlag(c *cobra.Command, target *string) {
	c.Flags().StringVarP(target, "dir", "d", lock.DefaultDir, "Skill directory; .agents/skills beside a skills-lock.json uses the lockfile, others SKILL.md metadata")
}

func newAddCmd() *cobra.Command {
	var (
		dryRunFlag bool
		yesFlag    bool
		skillFlag  []string
		dirFlag    string
	)

	c := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from a source repository (destructive when overwriting)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, args[0], dryRunFlag, yesFlag, skillFlag, dirFlag)
		},
	}

	c.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show the skills that would be installed without installing")
	c.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Overwrite existing skills without confirmation")
	c.Flags().StringSliceVarP(&skillFlag, "skill", "s", nil, "Install only specified skills (repeatable)")
	bindDirFlag(c, &dirFlag)

	return c
}

func newCheckCmd() *cobra.Command {
	var (
		dirFlag  string
		jsonFlag bool
	)

	c := &cobra.Command{
		Use:         "check",
		Short:       "Check installed skills for updates",
		Annotations: map[string]string{recordKey: "results"},
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, dirFlag, jsonFlag)
		},
	}

	bindDirFlag(c, &dirFlag)
	c.Flags().BoolVar(&jsonFlag, "json", false, "Output results in JSON format")

	return c
}

func newVerifyCmd() *cobra.Command {
	var (
		skillFlag []string
		dirFlag   string
		jsonFlag  bool
	)

	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify installed skills against their records",
		Long: `Verify installed skills against their records.

Lockfile skills are hashed on disk and compared with computedHash, offline.
gh skill metadata is compared file by file with the tree github-tree-sha names, fetched at github-ref.
Exits 1 when a skill is modified or cannot be verified.`,
		Annotations: map[string]string{recordKey: "results"},
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd, dirFlag, skillFlag, jsonFlag)
		},
	}

	c.Flags().StringSliceVarP(&skillFlag, "skill", "s", nil, "Verify only specified skills (repeatable)")
	bindDirFlag(c, &dirFlag)
	c.Flags().BoolVar(&jsonFlag, "json", false, "Output results in JSON format")

	return c
}

func newUpdateCmd() *cobra.Command {
	var (
		skillFlag  []string
		dirFlag    string
		yesFlag    bool
		dryRunFlag bool
	)

	c := &cobra.Command{
		Use:   "update [source]",
		Short: "Update installed skills from their sources (destructive)",
		Long: `Update installed skills from their sources (destructive).

Shows the diff of each skill, then overwrites local changes after confirmation.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) > 0 {
				source = args[0]
			}
			return runUpdate(cmd, source, skillFlag, dirFlag, yesFlag, dryRunFlag)
		},
	}

	c.Flags().StringSliceVarP(&skillFlag, "skill", "s", nil, "Update only specified skills (repeatable)")
	bindDirFlag(c, &dirFlag)
	c.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Apply updates without confirmation")
	c.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show the diffs without applying")

	return c
}

func newListCmd() *cobra.Command {
	var (
		dirFlag  string
		jsonFlag bool
	)

	c := &cobra.Command{
		Use:         "list",
		Aliases:     []string{"ls"},
		Annotations: map[string]string{recordKey: "skills"},
		Short:       "List installed skills",
		Args:        usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, dirFlag, jsonFlag)
		},
	}

	bindDirFlag(c, &dirFlag)
	c.Flags().BoolVar(&jsonFlag, "json", false, "Output results in JSON format")

	return c
}

func newRemoveCmd() *cobra.Command {
	var (
		dirFlag    string
		yesFlag    bool
		dryRunFlag bool
	)

	c := &cobra.Command{
		Use:     "remove <skill>...",
		Aliases: []string{"rm"},
		Short:   "Remove installed skills (destructive)",
		Args:    usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args, dirFlag, yesFlag, dryRunFlag)
		},
	}

	bindDirFlag(c, &dirFlag)
	c.Flags().BoolVarP(&yesFlag, "yes", "y", false, "Remove without confirmation")
	c.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show the directories that would be removed")

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
