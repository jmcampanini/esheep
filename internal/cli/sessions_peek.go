package cli

import (
	"errors"

	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsPeekCommand(load configLoader, operations commandOperations) *cobra.Command {
	var all bool
	var contextEvents int
	var id string
	var jsonOutput bool
	var lines []string
	var maxBytes int
	var subagents bool
	command := &cobra.Command{
		Use:   "peek --id <id> --line <lines>",
		Short: "Read the events on a session's transcript lines in full",
		Long: `Read the events on chosen lines of one session transcript with their
complete recorded text. 'sessions search' locates evidence and prints an
excerpt of each hit; peek reads a hit in full, with the events around it,
without parsing the harness's native transcript.

--id and --line are both required. --id is one exact, case-sensitive session
ID, as 'sessions list' and 'sessions search' print it. It selects among every
transcript, including subagent transcripts, whatever the archive state. An ID
that names no session fails with exit 1; so does an ID that names several
transcripts, with their paths in the message. Cowork accepts its full scoped
ID or its local_<id> component.

--line selects the events on one transcript line (12) or an inclusive range
of lines (12-20); repeat the flag or separate values with commas to select
several. --context adds that many events before and after the events of each
--line value. It counts events, not lines. An event selected more than once
is returned once.

Every --line value must return at least one event. Otherwise peek fails
with exit 1 and prints no events: the line is past the end of the
transcript, is not a decoded event, cannot be parsed, or holds child
activity that needs --subagents. 'sessions search --raw' reads lines that
are not decoded events.

A peek returns at most --max-bytes bytes of events (default 24000),
counting each returned event as its compact JSON encoding. The indented
JSON document and the session object add to that; text output is smaller.
Events on the --line lines are admitted first, in transcript order and a
whole line at a time, until a line's events do not fit. The first line is
always returned: when its events exceed the cap, their text is cut as far as
the cap needs, at a UTF-8 boundary, and the cap is exceeded only when it is
smaller than those events with no text. Context events are admitted next,
nearest first, and only when every event on the --line lines fit whole. The
report counts the selected events left out and names the line to resume at,
so a long range is read by repeating peek from that line. --all removes the
cap and cannot combine with --max-bytes, which must be at least 1. Stopping
at the cap exits 0.

Events are the decoded events that 'sessions search' matches: user and
assistant messages, tool calls, and tool results, including results with no
text. 'esheep sessions search --help' names the records each harness does
not decode. Each event's text is everything the transcript recorded for it:
the message text, a tool call's input, or a tool result's text blocks joined
by newlines. A tool result's content blocks that hold no text, such as an
image, are not carried: the event names their recorded types instead, and a
result holding only such blocks has empty text.

When a harness saved a shortened copy of an output, the text is that copy,
including any notice the harness wrote into it. Peek does not follow such a
notice to another file and does not mark the event: an event without
"text_cut" means only that peek did not cut it.

Events follow transcript line order, the order the harness recorded them.
That order is not a reconstructed conversation: results of concurrent tool
calls interleave, and where a harness records more than one branch in a
file, peek does not tell the branches apart.

--subagents returns Cowork child activity recorded inside the primary
audit. Without it those events are counted but not returned.

Session roots are read as 'sessions list' reads them, with the same
diagnostics. A missing root is skipped. When a root or a transcript that
could carry the ID cannot be read, the ID may also name a transcript peek
cannot see: peek still prints the events of the one session it found, and
exits 1. Unparseable transcript lines are skipped and reported as a
diagnostic without failing the command.

` + ui.StreamContractHelp + `

` + ui.JSONContractHelp + `

The JSON object contains "complete", false when a root or a transcript
that could carry the ID could not be read; "session", the session object
'sessions list' describes; "total_events", every event decoded from the
transcript;
"events", the returned events in transcript order; "omitted_events", the
selected events the cap left out; and "next_line", the transcript line of
the first --line event left out, or null when none was.

Each event contains "line", the 1-based transcript line number, which
several events can share; "position", the event's 1-based place among all of
the transcript's events, so consecutive positions mean adjacent events and a
jump means events are not shown; and "text", the recorded text. "text_cut":
true appears when the cap cut the text, with "text_bytes", the size of the
whole recorded text. "omitted_content" appears on a tool result whose
recorded content has blocks without text, listing each such block type once
as the harness names it, for example image. Each event also contains "kind",
"role", "origin", "tool", "timestamp", "error", "call_id", and
"parent_call_id" exactly as a search hit does;
'esheep sessions search --help' defines them.

Text output prints the session, the count of events shown, then each event
as a line starting with a colon and its line number, followed by its text
indented four spaces. That line carries omitted_content when the event has
it, and a note follows text the cap cut. A parenthesized count marks each
place where the session's other events are not shown. A final line reports
the selected events the cap left out and the line to resume at.`,
		Example: `  esheep sessions peek --id SESSION_ID --line 120 --context 3
  esheep sessions peek --id SESSION_ID --line 120,480-495 --json
  esheep sessions peek --id SESSION_ID --line 120 --all > event.txt`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request := session.RequestPeek{All: all, Context: contextEvents, ID: id, Lines: lines, Subagents: subagents}
			if command.Flags().Changed("max-bytes") {
				request.MaxBytes = &maxBytes
			}
			query, err := request.Resolve()
			if err != nil {
				return err
			}

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report, peekErr := operations.sessionPeek(command.Context(), sessionRoots(loaded), query)

			if peekErr == nil {
				write := ui.WriteSessionPeek
				if jsonOutput {
					write = ui.WriteSessionPeekJSON
				}
				if err := write(command.OutOrStdout(), report); err != nil {
					return err
				}
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return err
			}
			if peekErr == nil && !report.Complete {
				return errors.New("session peek is incomplete")
			}
			return peekErr
		},
	}
	command.Flags().BoolVar(&all, "all", false, "return every selected event with its whole text")
	command.Flags().IntVar(&contextEvents, "context", 0, "also return this many events before and after each selected line's events")
	command.Flags().StringVar(&id, "id", "", "the exact session ID to read")
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	command.Flags().StringSliceVar(&lines, "line", nil, "read the events on transcript lines (12) or line ranges (12-20); repeatable or comma-separated")
	command.Flags().IntVar(&maxBytes, "max-bytes", session.DefaultPeekBytes, "return at most this many bytes of events")
	command.Flags().BoolVar(&subagents, "subagents", false, "return embedded child activity")
	return command
}
