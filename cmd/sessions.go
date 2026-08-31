package cmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Find historical harness sessions",
		Long: `Find historical session transcripts recorded by Claude Code, Pi, and
Codex, reading the harness-owned files in place.

Transcripts are read-only inputs: esheep never creates, updates, or deletes
anything under a session root and keeps no copies or indexes. Every result
points at the canonical transcript file so the original can be read directly.

Session roots default to ~/.claude/projects, ~/.pi/agent/sessions, and
~/.codex/sessions and are configurable under [sessions] in the TOML file; a
missing root skips that harness with a diagnostic.

'sessions list' inventories sessions; 'sessions search' finds sessions whose
transcripts match a pattern or structural criteria.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newSessionsListCommand(load, operations.sessionList),
		newSessionsSearchCommand(load, operations.sessionSearch),
	)
	return command
}

// sessionFilterFlags carries the raw flag values shared by list and search.
type sessionFilterFlags struct {
	harnesses []string
	project   string
	since     string
	subagents bool
	until     string
}

func registerSessionFilterFlags(command *cobra.Command, flags *sessionFilterFlags) {
	command.Flags().StringSliceVar(&flags.harnesses, "harness", nil, "limit to harnesses (claude, codex, pi); repeatable or comma-separated")
	command.Flags().StringVar(&flags.project, "project", "", "limit to sessions whose project path contains this text")
	command.Flags().StringVar(&flags.since, "since", "", "limit to sessions active since a day count (7d), duration (36h), or date (2026-01-02)")
	command.Flags().BoolVar(&flags.subagents, "subagents", false, "include subagent and sidechain transcripts")
	command.Flags().StringVar(&flags.until, "until", "", "limit to sessions started before a day count, duration, or date")
}

func (f sessionFilterFlags) filter(now time.Time) (session.Filter, error) {
	filter := session.Filter{IncludeSubagents: f.subagents, Project: f.project}
	for _, name := range f.harnesses {
		harness, err := session.ParseHarness(name)
		if err != nil {
			return session.Filter{}, err
		}
		filter.Harnesses = append(filter.Harnesses, harness)
	}
	if f.since != "" {
		since, err := session.ParseTimeFlag(f.since, now)
		if err != nil {
			return session.Filter{}, fmt.Errorf("--since: %w", err)
		}
		filter.Since = since
	}
	if f.until != "" {
		until, err := session.ParseTimeFlag(f.until, now)
		if err != nil {
			return session.Filter{}, fmt.Errorf("--until: %w", err)
		}
		filter.Until = until
	}
	return filter, nil
}

func sessionRoots(loaded config.LoadResult) session.Roots {
	return session.Roots{
		Claude: loaded.ResolvedSessions.Claude,
		Codex:  loaded.ResolvedSessions.Codex,
		Pi:     loaded.ResolvedSessions.Pi,
	}
}

func newSessionsListCommand(load configLoader, list func(context.Context, session.Roots, session.Filter) session.ListReport) *cobra.Command {
	var filterFlags sessionFilterFlags
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List historical sessions, most recent first",
		Long: `List historical sessions under the configured session roots, most
recently started first, without touching the transcripts' content beyond a
short metadata read.

Each row carries the harness, start time, project directory, title where the
grammar records one, and the canonical transcript path. Subagent and
sidechain transcripts are excluded unless --subagents is set. --since keeps
sessions still active at the given time; --until drops sessions started
after it. Best-effort fields a grammar does not record appear as -.

The command exits nonzero only when filesystem failures prevent a complete
inventory; a missing session root merely skips that harness with a
diagnostic.

` + streamContractHelp + `

` + jsonContractHelp + ` List JSON includes "complete"; timestamps are
RFC 3339 and "subagent" marks non-primary transcripts.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			filter, err := filterFlags.filter(time.Now())
			if err != nil {
				return err
			}
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := list(command.Context(), sessionRoots(loaded), filter)
			if jsonOutput {
				if err := ui.WriteSessionListJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Complete {
					return silentAppError(errors.New("session inventory is incomplete"))
				}
				return nil
			}
			if err := ui.WriteSessionList(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if !report.Complete {
				return appError(errors.New("session inventory is incomplete"))
			}
			return nil
		},
	}
	registerSessionFilterFlags(command, &filterFlags)
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}

func newSessionsSearchCommand(load configLoader, search func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport) *cobra.Command {
	var errorsOnly bool
	var filterFlags sessionFilterFlags
	var jsonOutput bool
	var raw bool
	var role string
	var tool string
	command := &cobra.Command{
		Use:   "search [pattern]",
		Short: "Search session transcripts in their native files",
		Long: `Search historical session transcripts for matching events, reading each
harness's native grammar in place and printing hits as line numbers within
the canonical transcript file.

esheep decodes every transcript line before matching, so the pattern runs
against what was actually said or done, not escaped JSON: user text,
assistant text, and tool calls and results (tool arguments and output). The
pattern is a case-insensitive Go regular expression and is optional when
--tool or --errors already select events.

--role limits matching to user, assistant, or tool events. --tool limits to
calls of and results from one tool. --errors keeps only tool results whose
grammar flags a failure; Codex transcripts flag errors only on MCP calls, so
other failing Codex tool calls cannot match. --raw drops to byte-level
matching against the undecoded lines and cannot combine with --role, --tool,
or --errors.

Unparseable transcript lines are skipped and reported as diagnostics without
failing the search. The command exits nonzero only when filesystem failures
prevent a complete search.

` + streamContractHelp + `

` + jsonContractHelp + ` Search JSON includes "complete"; each session
carries "hits" with "line", "role", "excerpt", and, when known, "tool",
"error", and "timestamp".`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			query := session.SearchQuery{ErrorsOnly: errorsOnly, Raw: raw, Tool: tool}
			if role != "" {
				parsed, err := session.ParseRole(role)
				if err != nil {
					return err
				}
				query.Role = parsed
			}
			if len(args) == 1 {
				pattern, err := regexp.Compile("(?i)" + args[0])
				if err != nil {
					return fmt.Errorf("invalid pattern: %w", err)
				}
				query.Pattern = pattern
			}
			if query.Pattern == nil && query.Tool == "" && !query.ErrorsOnly {
				return errors.New("search requires a pattern, --tool, or --errors")
			}
			if raw && (query.Role != "" || query.Tool != "" || query.ErrorsOnly) {
				return errors.New("--raw cannot combine with --role, --tool, or --errors")
			}
			if raw && query.Pattern == nil {
				return errors.New("--raw requires a pattern")
			}
			filter, err := filterFlags.filter(time.Now())
			if err != nil {
				return err
			}
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := search(command.Context(), sessionRoots(loaded), filter, query)
			if jsonOutput {
				if err := ui.WriteSessionSearchJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Complete {
					return silentAppError(errors.New("session search is incomplete"))
				}
				return nil
			}
			if err := ui.WriteSessionSearch(command.OutOrStdout(), report); err != nil {
				return appError(err)
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if !report.Complete {
				return appError(errors.New("session search is incomplete"))
			}
			return nil
		},
	}
	registerSessionFilterFlags(command, &filterFlags)
	command.Flags().BoolVar(&errorsOnly, "errors", false, "keep only tool results flagged as failures")
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	command.Flags().BoolVar(&raw, "raw", false, "match the pattern against undecoded transcript lines")
	command.Flags().StringVar(&role, "role", "", "limit to events by role (user, assistant, tool)")
	command.Flags().StringVar(&tool, "tool", "", "limit to calls of and results from one tool")
	return command
}
