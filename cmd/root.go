package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
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
	list     func(context.Context, config.LoadResult) manage.ListReport
	profiles func(context.Context, config.LoadResult) manage.ProfilesReport
	status   func(context.Context, config.LoadResult) manage.StatusReport
	sync     func(context.Context, config.LoadResult) manage.SyncReport
}

type applicationError struct {
	err    error
	silent bool
}

func (e applicationError) Error() string {
	return e.err.Error()
}

func (e applicationError) Unwrap() error {
	return e.err
}

// Execute runs esheep with the process arguments and streams.
func Execute() int {
	return execute(newRootCommand(config.Load), os.Args[1:])
}

func execute(root *cobra.Command, args []string) int {
	root.SetArgs(args)
	failed, err := root.ExecuteC()
	if err == nil {
		return 0
	}

	exitCode := 2
	var application applicationError
	if errors.As(err, &application) {
		exitCode = 1
		if application.silent {
			return exitCode
		}
	}
	_, _ = fmt.Fprintf(root.ErrOrStderr(), "Error: %v\n", err)
	if exitCode == 2 && failed != nil {
		_, _ = fmt.Fprintf(root.ErrOrStderr(), "Run '%s --help' for usage.\n", failed.CommandPath())
	}
	return exitCode
}

func newRootCommand(load configLoader) *cobra.Command {
	return newRootCommandWithOperations(load, commandOperations{
		list:     manage.List,
		profiles: manage.Profiles,
		status:   manage.Status,
		sync:     manage.Sync,
	})
}

func newRootCommandWithOperations(load configLoader, operations commandOperations) *cobra.Command {
	root := &cobra.Command{
		Use:   "esheep",
		Short: "Manage local Agent Skills across coding harnesses",
		Long: `Manage Agent Skills from human-maintained local source directories.

esheep reads skills from configured sources and renders them for the Claude,
Pi, Codex, and shared agents targets. It never accesses the network, never
executes source content, and never creates, updates, or deletes source
directories. Commands accept only the arguments shown and never prompt.

Run 'esheep config' to inspect the effective configuration and resolved
paths, 'esheep help skill-format' for the authoring format, and
'esheep help exit-codes' for exit-status meanings.`,
		Version:       effectiveVersion(),
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	if err := config.RegisterFlags(root.PersistentFlags()); err != nil {
		panic(fmt.Sprintf("register configuration flags: %v", err))
	}
	root.AddCommand(
		newCompletionCommand(),
		newConfigCommand(load),
		newExitCodesTopic(),
		newProfilesCommand(load, operations.profiles),
		newSkillFormatTopic(),
		newSkillsCommand(load, operations),
		newSyncCommand(load, operations.sync),
	)
	return root
}

func loadConfiguration(command *cobra.Command, load configLoader) (config.LoadResult, error) {
	loaded, err := load(config.LoadOptions{Flags: command.Root().PersistentFlags()})
	if err != nil {
		return config.LoadResult{}, applicationError{err: err}
	}
	return loaded, nil
}

func appError(err error) error {
	if err == nil {
		return nil
	}
	return applicationError{err: err}
}

func silentAppError(err error) error {
	if err == nil {
		return nil
	}
	return applicationError{err: err, silent: true}
}
