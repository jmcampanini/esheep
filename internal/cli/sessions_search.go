package cli

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/remote"
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
--tool or --errors already select events. An empty pattern is a usage error;
use '.' to select every event.

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

` + sessionRemoteHelp + `

Unparseable transcript lines are skipped and reported as diagnostics without
failing the search. The command exits nonzero only when filesystem failures
prevent a complete search.

` + ui.StreamContractHelp + `

` + sessionPeriodHelp + `

With a period, hits are matching events within that period. A session that
started earlier contributes its qualifying events.

` + ui.JSONContractHelp + `

Search JSON includes "complete" and "period"; each session carries
"machine", "projects", an "archived" boolean, and a "hits" array. Each hit
carries "line", "role", and "excerpt"; "tool" and "timestamp" appear when
known, and "error" appears for known failures.`,
		Example: `  esheep sessions search timeout --harness codex --since 7d
  esheep sessions search . --id SESSION_ID
  esheep sessions search --errors --since 7d --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			names, err := remote.ParseNames(filterFlags.remote, filterFlags.noLocal)
			if err != nil {
				return err
			}
			requestFilter, err := filterFlags.requestFilter(time.Now())
			if err != nil {
				return err
			}
			request := session.Request{
				Filter: requestFilter,
				Mode:   session.ModeSearch,
				Query:  session.RequestQuery{Errors: errorsOnly, Pattern: strings.Join(args, ""), Raw: raw, Role: role, Tool: tool},
			}
			filter, query, err := request.Resolve()
			if err != nil {
				return err
			}

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			scope, err := resolveSessionScope(operations, loaded, names)
			if err != nil {
				return err
			}
			report, err := remote.Search(command.Context(), scope.ssh, scope.selection, request, func(ctx context.Context) session.SearchReport {
				return operations.sessionSearch(ctx, sessionRoots(loaded), filter, query)
			})
			if err != nil {
				return apperr.Wrap(err)
			}
			report.Diagnostics = append(scope.diagnostics, report.Diagnostics...)

			if jsonOutput {
				if err := ui.WriteSessionSearchJSON(command.OutOrStdout(), report); err != nil {
					return apperr.Wrap(err)
				}
				if !report.Complete {
					return apperr.WrapSilent(errors.New("session search is incomplete"))
				}
				return nil
			}
			machines := len(filterFlags.remote) != 0
			if err := ui.WriteSessionSearch(command.OutOrStdout(), report, machines); err != nil {
				return apperr.Wrap(err)
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics, machines); err != nil {
				return apperr.Wrap(err)
			}
			if !report.Complete {
				return apperr.Wrap(errors.New("session search is incomplete"))
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
