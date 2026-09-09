package cmd

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

const sessionHarnessHelp = `claude-cowork reads local Cowork conversations from
[sessions.claude-cowork].path. Its precedence is --claude-cowork-sessions-path,
ESHEEP_CLAUDE_COWORK_SESSIONS_PATH, TOML, then the platform default. On macOS,
the default is ~/Library/Application Support/Claude/local-agent-mode-sessions.
Linux requires an explicit path; an empty path disables Cowork discovery.
Only <scope>/<scope>/local_<id>/audit.jsonl files are discovered. Scope names
are opaque. Supporting files and regular Claude chats are not session sources.

The codex harness includes Codex CLI and desktop sessions. chatgpt-work
identifies local ChatGPT Work tasks whose recorded originator is exactly
codex_work_desktop. Missing or unrecognized originators are classified as codex.
Subagents use their own recorded originator.

Both harnesses share [sessions.codex].home. Its precedence is --codex-home,
ESHEEP_CODEX_HOME, TOML, CODEX_HOME, then ~/.codex. The sessions and
archived_sessions directories beneath that home are read in place; no other
home is scanned. Shared storage is discovered once; root diagnostics use
codex. Omit --harness to include all harnesses, or use
--harness codex,chatgpt-work to select both.

Work and Cowork transcripts qualify when they contain a supported user message,
assistant message, tool call, or tool result. Title-only and injected-context-only
records are excluded, including from --raw searches. Partial or unknown local
history qualifies; results do not imply complete remote history. A transcript's
history_base may reference earlier history elsewhere. That history is not
expanded or searched through the reference; hits address only saved lines in
each discovered file. Cowork uses its session folder's relative path, including
both scopes, as its ID. Companion metadata supplies title, creation time,
selected host folders, and the archive flag. Missing or invalid companions
do not exclude readable messages. Invalid or mismatched companions produce
diagnostics; a mismatched sessionId discards that companion's metadata.
Cowork event times prefer timestamp, then _audit_timestamp. A missing creation
time falls back to the first recorded event time.

Every session has a projects array. Claude Code and Pi supply their recorded
working directory. Cowork supplies userSelectedFolders, excluding its execution
working directory. Codex and Work supply the distinct workspace_roots across
saved turn_context records, falling back to the session header's cwd when no
workspace roots are recorded. --project matches any path, case-insensitively.
Unknown folders produce an empty array and do not match --project.

Active and archived transcripts are included by default. --archive-state
all|active|archived selects archive state independently of --subagents.
Cowork uses its companion's isArchived flag; absent or invalid evidence means
not archived. Claude and Pi are treated as active. Both Codex directories
are discovered before filtering so overlapping files have one archive state.
Repeated physical files, including hard links, appear once; archive placement
and its path win when a file occurs in both locations. Distinct files with
the same session ID remain separate.

Primary activity includes requests to subagents and their returned results.
--subagents additionally includes child activity, including Cowork records
marked with parent_tool_use_id inside the primary audit. An audit remains one
session result. --raw searches all original lines in included transcripts,
including embedded child activity, without decoding event roles.

Selecting a harness with no configured path reports a nonfatal diagnostic
naming its setting. Disabled harnesses are skipped quietly when --harness is
omitted. Missing locations are skipped with diagnostics. Unreadable inputs and
detected file moves make the scan incomplete; rerun after moves finish. Unreadable
Cowork companions make the scan incomplete while preserving readable hits.
Scans are not
atomic snapshots and do not retry. "complete" describes the filesystem scan,
not conversation or remote-history completeness.`

const sessionIDHelp = `--id selects exact, case-sensitive session IDs. Repeat the flag or use
comma-separated IDs to select any of them; all other filters still apply,
including --subagents. Empty IDs are usage errors. Cowork accepts either its
full scoped ID or its complete local_<id> component; the latter selects every
matching scope. Output retains full scoped IDs. Multiple files sharing an ID
remain separate results; no matches is a successful empty result when the scan
completes.

ID filtering rejects nonmatches before further metadata reads where possible.
Claude and Cowork use path-derived IDs. Codex and Work read the first-line
header, whose ID takes precedence over the filename fallback. Pi retains its
bounded metadata read. Matching sessions keep the same metadata and eligibility
rules as an unfiltered scan. Errors in skipped content or companion metadata
are not reported; discovery failures and failures reading required IDs or
matching transcripts still affect completeness.`

func newSessionsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Find historical harness sessions",
		Long: `Find historical session transcripts recorded by Claude Code, Pi,
Codex, local ChatGPT Work tasks, and local Claude Cowork conversations,
reading the harness-owned files in place.

Transcripts are read-only inputs: esheep never creates, updates, or deletes
anything under a session root and keeps no copies or indexes. Every result
points at the canonical transcript file so the original can be read directly.

Session roots default to ~/.claude/projects, ~/.pi/agent/sessions,
~/.codex/sessions, and ~/.codex/archived_sessions. Configure the Claude and Pi
paths and the Codex home under [sessions] in TOML; Codex transcript paths are
derived from its home. Cowork's platform defaults and configuration are
described below. A missing root skips that location with a diagnostic.

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
	ids          []string
	project      string
	since        string
	subagents    bool
	until        string
}

func registerSessionFilterFlags(command *cobra.Command, flags *sessionFilterFlags) {
	command.Flags().StringVar(&flags.archiveState, "archive-state", "all", "limit to archive state (all, active, archived)")
	command.Flags().StringSliceVar(&flags.harnesses, "harness", nil, "limit to harnesses (chatgpt-work, claude, claude-cowork, codex, pi); repeatable or comma-separated")
	command.Flags().StringArrayVar(&flags.ids, "id", nil, "limit to exact session IDs; repeatable or comma-separated")
	command.Flags().StringVar(&flags.project, "project", "", "limit to sessions with any project path containing this text")
	command.Flags().StringVar(&flags.since, "since", "", "limit to sessions active since a day count (7d), duration (36h), or date (2026-01-02)")
	command.Flags().BoolVar(&flags.subagents, "subagents", false, "include subagent transcripts and embedded child activity")
	command.Flags().StringVar(&flags.until, "until", "", "limit to sessions started before a day count, duration, or date")
}

func (f sessionFilterFlags) filter(now time.Time) (session.Filter, error) {
	archiveState, err := session.ParseArchiveState(f.archiveState)
	if err != nil {
		return session.Filter{}, err
	}
	filter := session.Filter{ArchiveState: archiveState, IncludeSubagents: f.subagents, Project: f.project}
	for _, value := range f.ids {
		if value == "" {
			return session.Filter{}, errors.New("--id must not be empty")
		}
		ids, err := csv.NewReader(strings.NewReader(value)).Read()
		if err != nil {
			return session.Filter{}, fmt.Errorf("--id: parse comma-separated IDs: %w", err)
		}
		for _, id := range ids {
			if id == "" {
				return session.Filter{}, errors.New("--id must not contain empty IDs")
			}
		}
		filter.IDs = append(filter.IDs, ids...)
	}
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
		ClaudeCowork:          loaded.ResolvedSessions.ClaudeCowork,
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
recently started first. Claude Code, Pi, and Cowork usually need only a short
metadata read. Codex and Work transcripts are read in full to collect workspace
roots across saved turns. Cowork audits are read until a supported conversation
event and a start time are found, or the end of the file.

Each row carries the harness, recorded start time (or file modification time
when unavailable), archive state, project directories, title where the grammar
records one, and the canonical transcript path. Subagent and
sidechain transcripts are excluded unless --subagents is set. --since keeps
sessions still active at the given time; --until drops sessions started
after it. Best-effort fields a grammar does not record appear as -.

The command exits nonzero only when filesystem failures prevent a complete
inventory; a missing session root merely skips that location with a
diagnostic.

` + sessionHarnessHelp + `

` + sessionIDHelp + `

` + streamContractHelp + `

` + jsonContractHelp + ` List JSON includes "complete"; timestamps are
RFC 3339, "projects" is an array of paths, "archived" marks archive state,
and "subagent" marks non-primary transcripts.`,
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

--id limits which sessions are searched; it does not select events. A pattern,
--tool, or --errors is still required. Use 'sessions list --id <id>' to locate
a session without searching its content.

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

Cowork decoded search supports user and assistant text, tool_use inputs,
and tool_result text. Thinking, images, documents, system events, rate-limit
events, and result summaries are not decoded search events. Only tool_result
is_error flags match --errors; a session-level result.is_error is not a tool
failure. Unknown tool names remain unset when the call is absent from the
saved history. Use --raw to inspect other records in qualifying audits.

` + sessionHarnessHelp + `

` + sessionIDHelp + `

Unparseable transcript lines are skipped and reported as diagnostics without
failing the search. The command exits nonzero only when filesystem failures
prevent a complete search.

` + streamContractHelp + `

` + jsonContractHelp + ` Search JSON includes "complete"; each session
carries "projects", an "archived" boolean, and a "hits" array. Each hit carries "line", "role",
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
