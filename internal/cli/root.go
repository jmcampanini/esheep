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
	memoryRecord  func(context.Context, memory.Request) (memory.Result, error)
	sessionList   func(context.Context, session.Roots, session.Filter) session.ListReport
	sessionSearch func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport
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
		memoryRecord:  memory.Record,
		sessionList:   session.List,
		sessionSearch: session.Search,
	})
}

func newRootCommandWithOperations(load configLoader, operations commandOperations) *cobra.Command {
	root := &cobra.Command{
		Use:   "esheep",
		Short: "Find harness session transcripts and record memories",
		Long: `Find historical coding-harness session transcripts and record memories
for the session in progress.

esheep reads Claude Code, Pi, Codex, local ChatGPT Work, and local Claude
Cowork transcripts in place and never modifies them. It writes only under
the memory root. Commands accept only the arguments shown and never
prompt. esheep contacts another machine only when --remote names it, and
only to run esheep there over ssh; every other command is offline.

External programs: ssh carries --remote session queries and must already
reach each named machine without a prompt; git 2.31 or later identifies
the project when a memory is recorded. Nothing else is required.

A bare 'esheep' prints this help on stdout. Every command writes its
payload to stdout and diagnostics to stderr; a failure ends with one
'esheep: <message>' line in every output mode.
Run 'esheep config' to inspect the effective configuration and resolved
paths, 'esheep config --help' for every configuration key, and 'esheep
help exit-codes' for exit statuses and the failure format.

Common tasks:
  List recent sessions:          esheep sessions list
  Find a session by content:     esheep sessions search 'budget review'
  Record a memory:               esheep memory record 'prefers table tests'

Command groups:
  memory    Memories recorded for harness sessions:  esheep memory --help
  sessions  Historical session transcripts:          esheep sessions --help`,
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
		newExitCodesTopic(),
		newMemoryCommand(load, operations.memoryRecord),
		newSessionsCommand(load, operations),
	)
	root.InitDefaultHelpCmd()
	for _, command := range root.Commands() {
		if command.Name() == "help" {
			command.Args = helpTopicArgs
			command.Run = nil
			command.RunE = runHelp
		}
	}
	return root
}

// helpTopicArgs accepts only operands that name one command or help topic.
func helpTopicArgs(command *cobra.Command, args []string) error {
	if _, remaining, err := command.Root().Find(args); err != nil || len(remaining) != 0 {
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	return nil
}

func runHelp(command *cobra.Command, args []string) error {
	target, _, err := command.Root().Find(args)
	if err != nil {
		return err
	}
	target.SetContext(command.Context())
	target.InitDefaultHelpFlag()
	target.InitDefaultVersionFlag()
	return apperr.Wrap(target.Help())
}
