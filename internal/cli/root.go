package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/memory"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/spf13/cobra"
)

// Version is replaced by the build with the repository revision.
var Version = "dev"

func effectiveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

type configLoader func(config.LoadOptions) (config.LoadResult, error)

type commandOperations struct {
	hostname      func() (string, error)
	list          func(context.Context, config.LoadResult) manage.ListReport
	memoryRecord  func(context.Context, memory.Request) (memory.Result, error)
	layers        func(context.Context, config.LoadResult) manage.LayersReport
	sessionList   func(context.Context, session.Roots, session.Filter) session.ListReport
	sessionSearch func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport
	status        func(context.Context, config.LoadResult) manage.StatusReport
	sync          func(context.Context, config.LoadResult) manage.SyncReport
}

// Execute runs esheep with the process arguments and streams.
func Execute() int {
	return execute(newRootCommand(config.Load), os.Args[1:])
}

// execute runs the tree and maps the outcome to a process status: 0 on
// success, 1 for an application failure, and 2 for a usage error. A
// failure prints one final line on stderr in the form '<cli>: <message>'.
func execute(root *cobra.Command, args []string) int {
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}

	exitCode := 2
	var application apperr.Error
	if errors.As(err, &application) {
		exitCode = 1
	}
	message := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(err.Error())
	_, _ = fmt.Fprintf(root.ErrOrStderr(), "%s: %s\n", root.Name(), message)
	return exitCode
}

func newRootCommand(load configLoader) *cobra.Command {
	return newRootCommandWithOperations(load, commandOperations{
		hostname:      os.Hostname,
		list:          manage.List,
		memoryRecord:  memory.Record,
		layers:        manage.Layers,
		sessionList:   session.List,
		sessionSearch: session.Search,
		status:        manage.Status,
		sync:          manage.Sync,
	})
}

func newRootCommandWithOperations(load configLoader, operations commandOperations) *cobra.Command {
	root := &cobra.Command{
		Use:   "esheep",
		Short: "Manage local Agent Skills and agents files across coding harnesses",
		Long: `Manage Agent Skills and a global agents file from human-maintained local
source directories, and find historical harness session transcripts.

esheep reads skills and an optional agents file from configured source
containers and renders them for the Claude, Pi, and Codex targets. It
never executes source content and never creates, updates, or deletes
source directories. Commands accept only the arguments shown and never
prompt. esheep contacts another machine only when --remote names it, and
only to run esheep there over ssh; every other command is offline.

External programs: ssh carries --remote session queries and must already
reach each named machine without a prompt; git 2.31 or later identifies
the project when a memory is recorded. Nothing else is required.

A bare 'esheep' prints this help on stdout. Every command writes its
payload to stdout and diagnostics to stderr; a failure ends with one
'esheep: <message>' line in every output mode.
Run 'esheep config' to inspect the effective configuration and resolved
paths, 'esheep config --help' for every configuration key, 'esheep help
skill-format' for the authoring format, and 'esheep help exit-codes' for
exit statuses and the failure format.

Common tasks:
  Deploy changed source skills:  esheep sync
  Check deployment health:       esheep skills status
  Find a session by content:     esheep sessions search 'budget review'

Command groups:
  memory    Memories recorded for harness sessions:  esheep memory --help
  sessions  Historical session transcripts:          esheep sessions --help
  skills    Known skills and deployment status:      esheep skills --help`,
		Version:            effectiveVersion(),
		DisableSuggestions: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	// Cobra never wraps flag usage; wrapping it at 80 columns keeps every
	// help line within a default terminal.
	root.SetUsageTemplate(strings.NewReplacer(
		".LocalFlags.FlagUsages", ".LocalFlags.FlagUsagesWrapped 80",
		".InheritedFlags.FlagUsages", ".InheritedFlags.FlagUsagesWrapped 80",
	).Replace(root.UsageTemplate()))
	if err := config.RegisterFlags(root.PersistentFlags()); err != nil {
		panic(fmt.Sprintf("register configuration flags: %v", err))
	}
	registerColorFlag(root.PersistentFlags())
	// Cobra registers --help and --version only when it executes the found
	// command, but it strips flags while finding it. Registering them here keeps
	// 'esheep --help --config x' from reading "x" as an unknown command.
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	root.AddCommand(
		newCompletionCommand(),
		newConfigCommand(load),
		newDoctorCommand(load),
		newExitCodesTopic(),
		newMemoryCommand(load, operations.memoryRecord),
		newLayersCommand(load, operations.layers),
		newSessionsCommand(load, operations),
		newSkillFormatTopic(),
		newSkillsCommand(load, operations),
		newSyncCommand(load, operations.sync),
	)
	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Args = cobra.ArbitraryArgs
			command.Run = nil
			command.RunE = runHelp
		}
	}
	return root
}

func runHelp(command *cobra.Command, args []string) error {
	target, remaining, err := command.Root().Find(args)
	if err != nil || len(remaining) != 0 {
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	target.SetContext(command.Context())
	target.InitDefaultHelpFlag()
	target.InitDefaultVersionFlag()
	return apperr.Wrap(target.Help())
}
