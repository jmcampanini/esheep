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
--tool or --errors already select events. An empty pattern fails with exit 1;
use '.' to select every event, which with --id prints one session's events
with their line numbers.

--id limits which sessions are searched; it does not select events. A pattern,
--tool, or --errors is still required. 'sessions list' with --id locates a
session without searching its content.

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

Search sessions also contain "hits", an array of matching events. Each hit
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
  esheep sessions search . --id SESSION_ID
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

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := operations.sessionSearch(command.Context(), sessionRoots(loaded), filter, query)

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
	command.Flags().BoolVar(&errorsOnly, "errors", false, "keep only tool results flagged as failures")
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	command.Flags().BoolVar(&raw, "raw", false, "match the pattern against undecoded transcript lines")
	command.Flags().StringVar(&role, "role", "", "limit to events by role (user, assistant, tool)")
	command.Flags().StringVar(&tool, "tool", "", "limit to calls of and results from one tool")
	return command
}
