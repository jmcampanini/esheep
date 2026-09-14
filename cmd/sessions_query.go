package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jmcampanini/esheep/internal/remote"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsQueryCommand(load configLoader, operations commandOperations) *cobra.Command {
	return &cobra.Command{
		Use:   "query",
		Short: "Answer a session request read from stdin",
		Long: `Answer one list or search request read as JSON from stdin, using this
machine's own configuration and transcripts. This is the machine-to-machine
entry point that 'sessions list --remote' and 'sessions search --remote'
run over ssh; it never consults [[machines]] itself. A file on stdin drives
it by hand.

The request mirrors the list and search flags:

  {
    "mode": "search",
    "filter": {
      "archive_state": "all",
      "harnesses": [],
      "ids": [],
      "project": "",
      "since": "2026-09-06T21:47:06-04:00",
      "until": null,
      "subagents": false
    },
    "query": {"pattern": "timeout", "role": "", "tool": "", "errors": false, "raw": false}
  }

"mode" is list or search. "filter" carries every shared filter: archive_state
is all, active, or archived; harnesses and ids are arrays of the values the
flags accept; since and until are RFC 3339 instants or null, already resolved
on the caller's clock so a skewed clock here cannot move the window. "query"
is ignored for list; pattern is the case-insensitive regular expression
source. Unknown fields are ignored and missing fields take their zero value.

Validation matches the flags: a search needs a pattern, tool, or errors;
raw cannot combine with role, tool, or errors; empty IDs are rejected. An
invalid or empty request exits 2 with the reason on stderr.

Stdout is exactly the document 'sessions list --json' or 'sessions search
--json' prints here, including "complete", "diagnostics", and "sessions",
with "machine" naming this host. Exit status follows the underlying
command: 0 complete, 1 incomplete or application failure, 2 usage.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			data, err := io.ReadAll(command.InOrStdin())
			if err != nil {
				return appError(fmt.Errorf("read request: %w", err))
			}
			if len(bytes.TrimSpace(data)) == 0 {
				return errors.New("request required on stdin; see 'esheep sessions query --help' for the schema")
			}
			var request session.Request
			if err := json.Unmarshal(data, &request); err != nil {
				return fmt.Errorf("decode request: %w", err)
			}
			filter, query, err := request.Resolve()
			if err != nil {
				return err
			}

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			scope, err := resolveSessionScope(operations, loaded, remote.Names{})
			if err != nil {
				return err
			}
			roots := sessionRoots(loaded)
			if request.Mode == session.ModeList {
				report, err := remote.List(command.Context(), scope.ssh, scope.selection, request, func(ctx context.Context) session.ListReport {
					return operations.sessionList(ctx, roots, filter)
				})
				if err != nil {
					return appError(err)
				}
				if err := ui.WriteSessionListJSON(command.OutOrStdout(), report); err != nil {
					return appError(err)
				}
				if !report.Complete {
					return silentAppError(errors.New("session inventory is incomplete"))
				}
				return nil
			}
			report, err := remote.Search(command.Context(), scope.ssh, scope.selection, request, func(ctx context.Context) session.SearchReport {
				return operations.sessionSearch(ctx, roots, filter, query)
			})
			if err != nil {
				return appError(err)
			}
			if err := ui.WriteSessionSearchJSON(command.OutOrStdout(), report); err != nil {
				return appError(err)
			}
			if !report.Complete {
				return silentAppError(errors.New("session search is incomplete"))
			}
			return nil
		},
	}
}
