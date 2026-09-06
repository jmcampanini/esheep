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

const sessionHarnessHelp = `The codex harness includes Codex CLI and desktop sessions. chatgpt-work
identifies local ChatGPT Work tasks whose recorded originator is exactly
codex_work_desktop. Missing or unrecognized originators are classified as codex.
Subagents use their own recorded originator.

Both harnesses share [sessions.codex].home. Its precedence is --codex-home,
ESHEEP_CODEX_HOME, TOML, CODEX_HOME, then ~/.codex. The sessions and
archived_sessions directories beneath that home are read in place; no other
home is scanned. Shared storage is discovered once; root diagnostics use
codex. Omit --harness to include all harnesses, or use
--harness codex,chatgpt-work to select both.

Work transcripts qualify when they contain a supported user message,
assistant message, tool call, or tool result. Title-only and injected-context-only
records are excluded, including from --raw searches. Partial or unknown local
history qualifies; results do not imply complete remote history. A transcript's
history_base may reference earlier history elsewhere. That history is not
expanded or searched through the reference; hits address only saved lines in
each discovered file.

Active and archived transcripts are included by default. --archive-state
all|active|archived selects archive placement independently of --subagents.
Claude and Pi are treated as active for this filter. Both Codex directories
are discovered before filtering so overlapping files have one archive state.
Repeated physical files, including hard links, appear once; archive placement
and its path win when a file occurs in both locations. Distinct files with
the same session ID remain separate.

Missing locations are skipped with diagnostics. Unreadable inputs and detected
file moves make the scan incomplete; rerun after moves finish. Scans are not
atomic snapshots and do not retry. "complete" describes the filesystem scan,
not conversation or remote-history completeness.`

func newSessionsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Find historical harness sessions",
		Long: `Find historical session transcripts recorded by Claude Code, Pi,
Codex, and local ChatGPT Work tasks, reading the harness-owned files in place.

Transcripts are read-only inputs: esheep never creates, updates, or deletes
anything under a session root and keeps no copies or indexes. Every result
points at the canonical transcript file so the original can be read directly.

Session roots default to ~/.claude/projects, ~/.pi/agent/sessions,
~/.codex/sessions, and ~/.codex/archived_sessions. Configure the Claude and Pi
paths and the Codex home under [sessions] in TOML; Codex transcript paths are
derived from its home. A missing root skips that location with a diagnostic.

'sessions list' inventories sessions; 'sessions search' finds sessions whose
transcripts match a pattern or structural criteria.

` + sessionHarnessHelp,
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
	archiveState string
	harnesses    []string
	project      string
	since        string
	subagents    bool
	until        string
}

func registerSessionFilterFlags(command *cobra.Command, flags *sessionFilterFlags) {
	command.Flags().StringVar(&flags.archiveState, "archive-state", "all", "limit to archive placement (all, active, archived)")
	command.Flags().StringSliceVar(&flags.harnesses, "harness", nil, "limit to harnesses (chatgpt-work, claude, codex, pi); repeatable or comma-separated")
	command.Flags().StringVar(&flags.project, "project", "", "limit to sessions whose project path contains this text")
	command.Flags().StringVar(&flags.since, "since", "", "limit to sessions active since a day count (7d), duration (36h), or date (2026-01-02)")
	command.Flags().BoolVar(&flags.subagents, "subagents", false, "include subagent and sidechain transcripts")
	command.Flags().StringVar(&flags.until, "until", "", "limit to sessions started before a day count, duration, or date")
}

func (f sessionFilterFlags) filter(now time.Time) (session.Filter, error) {
	archiveState, err := session.ParseArchiveState(f.archiveState)
	if err != nil {
		return session.Filter{}, err
	}
	filter := session.Filter{ArchiveState: archiveState, IncludeSubagents: f.subagents, Project: f.project}
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
		Claude:                loaded.ResolvedSessions.Claude,
		CodexArchivedSessions: loaded.ResolvedSessions.Codex.ArchivedSessions,
		CodexSessions:         loaded.ResolvedSessions.Codex.Sessions,
		Pi:                    loaded.ResolvedSessions.Pi,
	}
}

func newSessionsListCommand(load configLoader, list func(context.Context, session.Roots, session.Filter) session.ListReport) *cobra.Command {
	var filterFlags sessionFilterFlags
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List historical sessions, most recent first",
		Long: `List historical sessions under the configured session roots, most
recently started first. Most transcripts need only a short metadata read.
Work transcripts are read until the first qualifying conversation event or
the end of the file.

Each row carries the harness, recorded start time (or file modification time
when unavailable), archive state, project directory, title where the grammar
records one, and the canonical transcript path. Subagent and
sidechain transcripts are excluded unless --subagents is set. --since keeps
sessions still active at the given time; --until drops sessions started
after it. Best-effort fields a grammar does not record appear as -.

The command exits nonzero only when filesystem failures prevent a complete
inventory; a missing session root merely skips that location with a
diagnostic.

` + sessionHarnessHelp + `

` + streamContractHelp + `

` + jsonContractHelp + ` List JSON includes "complete"; timestamps are
RFC 3339, "archived" marks archive placement, and "subagent" marks
non-primary transcripts.`,
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
grammar flags a failure; Codex and ChatGPT Work transcripts flag errors only
on MCP calls, so other failing tool calls in those transcripts cannot match.
--raw drops to byte-level
matching against the undecoded lines and cannot combine with --role, --tool,
or --errors.

Codex and ChatGPT Work limitations: decoded search omits web-search events
and may report one MCP call twice under different tool names. Use --raw to
inspect those records in qualifying transcripts.

` + sessionHarnessHelp + `

Unparseable transcript lines are skipped and reported as diagnostics without
failing the search. The command exits nonzero only when filesystem failures
prevent a complete search.

` + streamContractHelp + `

` + jsonContractHelp + ` Search JSON includes "complete"; each session
carries an "archived" boolean and a "hits" array. Each hit carries "line", "role",
and "excerpt"; "tool" and "timestamp" appear when known, and "error" appears
for known failures.`,
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
			if query.Role != "" && query.Role != session.RoleTool && (query.Tool != "" || query.ErrorsOnly) {
				return errors.New("--role user or assistant cannot combine with --tool or --errors")
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
