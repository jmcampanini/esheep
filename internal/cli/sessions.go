package cli

import (
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/spf13/cobra"
)

const sessionHarnessHelp = `claude-cowork reads local Cowork conversations from
[sessions.claude-cowork].path. Its precedence is --claude-cowork-sessions-path,
ESHEEP_CLAUDE_COWORK_SESSIONS_PATH, TOML, then the platform default. On macOS,
the default is ~/Library/Application Support/Claude/local-agent-mode-sessions.
Linux requires an explicit path; an empty path disables Cowork discovery.
Only <scope>/<scope>/local_<id>/audit.jsonl files are discovered. Scope names
are opaque. The adjacent <scope>/<scope>/local_<id>.json file supplies companion
metadata. Supporting files and regular Claude chats are not session sources.

The codex harness includes Codex CLI and desktop sessions. chatgpt-work
identifies local ChatGPT Work tasks whose recorded originator is exactly
codex_work_desktop. Missing or unrecognized originators are classified as codex.
Subagents use their own recorded originator.

Both harnesses share [sessions.codex].home. Its precedence is --codex-home,
ESHEEP_CODEX_HOME, CODEX_HOME, TOML, then ~/.codex. The sessions and
archived_sessions directories beneath that home are read in place; no other
home is scanned. Shared storage is discovered once; root diagnostics use
codex. Omit --harness to include all harnesses, or use
--harness codex,chatgpt-work to select both.

Work and Cowork transcripts qualify when they contain a supported user message,
assistant message, tool call, or tool result. Title-only and
injected-context-only records are excluded, including from --raw searches.
Partial or unknown local history qualifies; results do not imply complete
remote history. A transcript's history_base may reference earlier history
elsewhere. That history is not expanded or searched through the reference;
hits address only saved lines in each discovered file. Cowork uses its
session folder's relative path, including both scopes, as its ID.
Companion metadata supplies title, creation time,
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
detected file moves make the scan incomplete; rerun after moves finish.
Unreadable Cowork companions make the scan incomplete while preserving
readable hits. Scans are not atomic snapshots and do not retry. "complete"
describes the filesystem scan, not conversation or remote-history
completeness.`

const sessionIDHelp = `--id selects exact, case-sensitive session IDs. Repeat the flag or use
comma-separated IDs to select any of them; all other filters still apply,
including --subagents. Empty IDs fail with exit 1. Cowork accepts either its
full scoped ID or its complete local_<id> component; the latter selects every
matching scope. Output retains full scoped IDs. Multiple files sharing an ID
remain separate results; no matches is a successful empty result when the scan
completes.

ID values cannot contain line breaks. Values use CSV quoting: to select the
ID a"b,c, pass --id '"a""b,c"'.

ID filtering rejects nonmatches before further metadata reads where possible.
Claude and Cowork use path-derived IDs. Codex and Work read the first-line
header, whose ID takes precedence over the filename fallback. Pi retains its
bounded metadata read. Matching sessions keep the same metadata and eligibility
rules as an unfiltered scan. Errors in skipped content or companion metadata
are not reported; discovery failures and failures reading required IDs or
matching transcripts still affect completeness.`

const sessionJSONHelp = `The JSON object contains "complete", "period", and "sessions". An empty
result has "sessions": []. Each session contains "harness" and "id" strings,
"path" naming the canonical transcript, "projects" as an array of directory
paths, "archived" and "subagent" booleans, and "modified_at" as an RFC 3339
timestamp. "started_at" is an RFC 3339 timestamp
omitted when unknown; "title" is a string omitted when unavailable. Sessions
sort by start time, falling back to modification time, most recent first.

Each session has an "origin" object with "kind": unknown, human, automation,
agent, or subagent. This describes recorded initiation, independently of each
event's origin. Subagent origin follows the same markers as "subagent" and
--subagents. Claude and Pi otherwise have unknown session origin; Cowork
session origin is unknown even when the audit includes child activity.

Codex and Work use source.subagent first, then source.internal (agent), then
thread_source: user means human, automation means automation, and
memory_consolidation means agent. Other values mean unknown. thread_source
is the harness's analytics classification: human means recorded user initiation,
not independently verified human action. Automation does not distinguish a
scheduled run from a manual run. Source strings such as cli, vscode, and exec,
and the originator product name, do not establish initiation.

Origin objects may include "evidence", a map of native field paths to recorded
string or number values rendered as strings. Codex and Work session evidence
comes from source, thread_source, and originator. Nested object keys become
dotted paths. Nulls, booleans, and arrays are omitted. Evidence is bounded to
four object descents per root and 32 entries in sorted key order. Keys longer
than 256 bytes are omitted; values longer than 256 bytes are shortened at a
UTF-8 boundary with a trailing '...' inside that limit. Evidence is not a
complete copy of native metadata. Text output also shows origins and evidence.`

const sessionPeriodHelp = `--since and --until select activity by event timestamps recorded in each
transcript. File modification time and session start do not determine period
membership or skip candidate transcripts. An event qualifies when
since <= timestamp < until; omitted bounds are unrestricted.

Dates are inclusive local calendar days. --since starts at local midnight on
that date; --until ends at local midnight on the next date. Day counts (7d)
and durations (36h) subtract elapsed time from one clock reading and resolve
to exact instants at either bound. A since at or after until is a failure with
exit 1 naming both resolved instants.

Under a period, undated events never qualify. Search reports undated-events
for events that match its other criteria but lack usable timestamps. List
reports it when undated events exist and none qualify before EOF. These
diagnostics do not change complete. List stops at its first qualifying event
and reports malformed-lines only for the portion scanned. --raw cannot combine
with --since or --until.

List and search JSON always include period with resolved since and until
instants, each null when open.`

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

A bare 'esheep sessions' prints this help on stdout. Every command in
this group writes its report to stdout and diagnostics to stderr, offers
the report as one JSON document, and never prompts.

` + sessionHarnessHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newSessionsListCommand(load, operations),
		newSessionsSearchCommand(load, operations),
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
	command.Flags().StringVar(&flags.archiveState, "archive-state", config.DefaultArchiveState, "limit to archive state (all, active, archived)")
	command.Flags().StringSliceVar(&flags.harnesses, "harness", nil, "limit to harnesses (chatgpt-work, claude, claude-cowork, codex, pi); repeatable or comma-separated")
	command.Flags().StringArrayVar(&flags.ids, "id", nil, "limit to exact session IDs; repeatable or comma-separated")
	command.Flags().StringVar(&flags.project, "project", "", "limit to sessions with any project path containing this text")
	command.Flags().StringVar(&flags.since, "since", "", "keep activity recorded at or after a day count (7d), duration (36h), or local date (2026-01-02)")
	command.Flags().BoolVar(&flags.subagents, "subagents", false, "include subagent transcripts and embedded child activity")
	command.Flags().StringVar(&flags.until, "until", "", "keep activity recorded before a day count, duration, or the end of a local date")
}

// requestFilter converts the flag values into the request form, resolving
// relative times against now.
func (f sessionFilterFlags) requestFilter(now time.Time) (session.RequestFilter, error) {
	filter := session.RequestFilter{ArchiveState: f.archiveState, Harnesses: append([]string{}, f.harnesses...), IDs: []string{}, Project: f.project, Subagents: f.subagents}
	for _, value := range f.ids {
		if value == "" {
			return session.RequestFilter{}, errors.New("--id must not be empty")
		}
		if strings.ContainsAny(value, "\r\n") {
			return session.RequestFilter{}, errors.New("--id must not contain line breaks")
		}
		ids, err := csv.NewReader(strings.NewReader(value)).Read()
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--id: parse comma-separated IDs: %w", err)
		}
		filter.IDs = append(filter.IDs, ids...)
	}
	if f.since != "" {
		since, err := session.ParseSince(f.since, now)
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--since: %w", err)
		}
		filter.Since = &since
	}
	if f.until != "" {
		until, err := session.ParseUntil(f.until, now)
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--until: %w", err)
		}
		filter.Until = &until
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
