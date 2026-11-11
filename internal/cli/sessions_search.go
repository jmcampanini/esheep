package cli

import (
	"errors"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsSearchCommand(load configLoader, operations commandOperations) *cobra.Command {
	var all bool
	var cursor string
	var errorsOnly bool
	var filterFlags sessionFilterFlags
	var hitLimit int
	var jsonOutput bool
	var limit int
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
--tool or --errors already select events. An empty pattern fails with exit 1;
use '.' to select every event that has text. A hit carries an excerpt, not
the whole event: 'sessions peek' prints a hit's complete text and the
events around it.

--id limits which sessions are searched; it does not select events. A pattern,
--tool, or --errors is still required. 'sessions list' with --id locates a
session without searching its content.

Search always scans every selected transcript, then returns one page of the
results. Sessions follow the documented session order below; hits within a
session follow transcript line order. A page holds at most --limit sessions
(default 10), each with its first --hit-limit hits (default 5). --all returns
every matching session with every hit and cannot combine with --limit or
--hit-limit; both limits must be at least 1. When more sessions remain, the
report carries a cursor: pass it to --cursor with the same search to return
the sessions after that page. A cursor marks a position in the session order,
so sessions that start after it was issued appear only on a new first page,
and a cursor that names no remaining position returns an empty page. To see
more hits from one session, select it with --id and raise --hit-limit.
Stopping at a limit exits 0.

--role limits matching to user, assistant, or tool events. --tool limits to
calls of and results from one tool. --errors keeps only tool results whose
grammar flags a failure; Codex and ChatGPT Work transcripts flag errors only
on MCP calls, so other failing tool calls in those transcripts cannot match.
--raw drops to byte-level
matching against the undecoded lines and cannot combine with --role, --tool,
--errors, --since, or --until.

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
failing the search. The command exits nonzero when filesystem failures
prevent a complete search.

` + ui.StreamContractHelp + `

` + sessionPeriodHelp + `

With a period, hits are matching events within that period. A session that
started earlier contributes its qualifying events.

` + ui.JSONContractHelp + `

` + sessionJSONHelp + `

Search JSON also contains "matched_sessions" and "matched_hits", counting every
match in the whole search whatever the page; "remaining_sessions", the matching
sessions after this page; "has_more", true when that count is positive; and
"next_cursor", the --cursor value for the next page, or null when has_more is
false. "sources" lists each session root the search considered, ordered by
harness and path, with "harness", "path" (omitted for a disabled harness),
"status" (searched, missing, unusable, or disabled), and "matched_sessions"
from that root. Roots excluded by --harness or --archive-state are not listed.
Counts cover only readable history: "complete" still reports whether the scan
read every selected file.

Text output begins with the same summary: matched and returned counts, the
sources, and the next cursor when more sessions remain. A session whose hits
were cut ends with a line counting the omitted hits.

Search sessions also contain "hits", the returned matching events;
"matched_hits", every matching event in the session; and "omitted_hits", the
matching events this page left out. Each hit
contains "line", the 1-based transcript line number; "role", the event role;
"kind", one of message, tool_call, tool_result, or raw; and "excerpt", a
window of up to 160 bytes starting up to 40 bytes before
the match, expanded to UTF-8 boundaries with whitespace collapsed and
ellipses marking omitted text. Without a text match the window starts at
the beginning. "tool" names a known tool, "timestamp" is an RFC 3339 event
time when known, and "error": true appears only for a known tool failure.

Each hit has its own "origin" object with "kind": unknown, human, agent,
harness, or tool. Role describes the transcript channel, not human authorship.
Assistant messages and tool calls have agent origin; results have tool origin,
meaning the recorded tool-result channel (including harness error text).
Claude and Cowork user-message origin.kind maps human to human,
task-notification to harness, and peer or coordinator to agent. Absent or null
origin falls back to turnOrigin: human, peer, and task_notification map to
human, agent, and harness. Conflicting mapped origins mean unknown. Evidence
includes only the origin.kind scalar, turnOrigin, and promptSource;
promptSource alone never establishes authorship. Unknown labels remain unknown.
Codex user.text, legacy user messages, and Pi user messages have unknown origin.
Injected text remains excluded according to the harness metadata; a non-meta
task notification is a message with harness origin. Raw hits have unknown
origin and no decoded call relationships.

"call_id", when recorded, identifies a tool call or the call a result belongs
to. "parent_call_id" identifies the enclosing call for every Cowork child
event, including messages. It names the spawning call in the enclosing scope,
not the event's own call. Associate calls and results by canonical path,
parent_call_id, and nonempty call_id, never by adjacency or tool name.
An absent parent_call_id denotes the main scope. A missing call_id means the
relationship is unknown. A call may have zero, one, or multiple results, and
a result may refer to a call absent from the saved history. Search filters
may return only one side of a relationship. Text output labels calls as
call:<tool> and results as result:<tool>, using ? for an unknown tool name,
and shows origin and recorded call IDs.`,
		Example: `  esheep sessions search timeout --harness codex --since 7d
  esheep sessions search timeout --since 7d --cursor CURSOR
  esheep sessions search . --id SESSION_ID --hit-limit 50
  esheep sessions search --errors --since 7d --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			requestFilter, err := filterFlags.requestFilter(time.Now())
			if err != nil {
				return err
			}
			requestQuery := session.RequestQuery{Errors: errorsOnly, Pattern: strings.Join(args, ""), Raw: raw, Role: role, Tool: tool}
			filter, query, err := session.ResolveSearch(requestFilter, requestQuery)
			if err != nil {
				return err
			}
			page, err := pageFlags(command, all, cursor, hitLimit, limit).Resolve()
			if err != nil {
				return err
			}

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := operations.sessionSearch(command.Context(), sessionRoots(loaded), filter, query).Paginate(page)

			if jsonOutput {
				if err := ui.WriteSessionSearchJSON(command.OutOrStdout(), report); err != nil {
					return err
				}
			} else if err := ui.WriteSessionSearch(command.OutOrStdout(), report); err != nil {
				return err
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return err
			}
			if !report.Complete {
				return errors.New("session search is incomplete")
			}
			return nil
		},
	}
	registerSessionFilterFlags(command, &filterFlags)
	command.Flags().BoolVar(&all, "all", false, "return every matching session and hit")
	command.Flags().StringVar(&cursor, "cursor", "", "return the sessions after the page that printed this cursor")
	command.Flags().BoolVar(&errorsOnly, "errors", false, "keep only tool results flagged as failures")
	command.Flags().IntVar(&hitLimit, "hit-limit", session.DefaultHitLimit, "return at most this many hits per session")
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	command.Flags().IntVar(&limit, "limit", session.DefaultLimit, "return at most this many sessions")
	command.Flags().BoolVar(&raw, "raw", false, "match the pattern against undecoded transcript lines")
	command.Flags().StringVar(&role, "role", "", "limit to events by role (user, assistant, tool)")
	command.Flags().StringVar(&tool, "tool", "", "limit to calls of and results from one tool")
	return command
}

// pageFlags converts the page flag values into the request form, leaving
// unset flags nil so defaults and conflicts resolve in one place.
func pageFlags(command *cobra.Command, all bool, cursor string, hitLimit, limit int) session.RequestPage {
	page := session.RequestPage{All: all}
	if command.Flags().Changed("cursor") {
		page.Cursor = &cursor
	}
	if command.Flags().Changed("hit-limit") {
		page.HitLimit = &hitLimit
	}
	if command.Flags().Changed("limit") {
		page.Limit = &limit
	}
	return page
}
