package cli

import (
	"context"
	"errors"
	"time"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/remote"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsListCommand(load configLoader, operations commandOperations) *cobra.Command {
	var filterFlags sessionFilterFlags
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List historical sessions, most recent first",
		Long: `List historical sessions under the configured session roots, most
recently started first. Claude Code, Pi, and Cowork usually need only a short
metadata read. Codex and Work transcripts are read in full to collect workspace
roots across saved turns. Cowork audits are read until a supported conversation
event and a start time are found, or the end of the file. With a period,
each candidate transcript is also scanned until its first event in the period
or the end of the file.

Each row carries the harness, session ID, recorded start time (or file
modification time when unavailable), archive state, project directories, title where the grammar
records one, and the canonical transcript path. Subagent and
sidechain transcripts are excluded unless --subagents is set. --since and
--until keep sessions with at least one recorded event in the period.
Best-effort fields a grammar does not record appear as -.

The command exits nonzero only when filesystem failures prevent a complete
inventory; a missing session root merely skips that location with a
diagnostic.

` + sessionHarnessHelp + `

` + sessionIDHelp + `

` + sessionRemoteHelp + `

` + ui.StreamContractHelp + `

` + sessionPeriodHelp + `

` + ui.JSONContractHelp + ` List JSON includes "complete" and "period"; timestamps are
RFC 3339, "projects" is an array of paths, "archived" marks archive state,
"subagent" marks non-primary transcripts, and "machine" names the machine
that owns each session and diagnostic.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			names, err := remote.ParseNames(filterFlags.remote, filterFlags.noLocal)
			if err != nil {
				return err
			}
			requestFilter, err := filterFlags.requestFilter(time.Now())
			if err != nil {
				return err
			}
			request := session.Request{Filter: requestFilter, Mode: session.ModeList}
			filter, _, err := request.Resolve()
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
			report, err := remote.List(command.Context(), scope.ssh, scope.selection, request, func(ctx context.Context) session.ListReport {
				return operations.sessionList(ctx, sessionRoots(loaded), filter)
			})
			if err != nil {
				return apperr.Wrap(err)
			}
			report.Diagnostics = append(scope.diagnostics, report.Diagnostics...)

			if jsonOutput {
				if err := ui.WriteSessionListJSON(command.OutOrStdout(), report); err != nil {
					return apperr.Wrap(err)
				}
				if !report.Complete {
					return apperr.WrapSilent(errors.New("session inventory is incomplete"))
				}
				return nil
			}
			machines := len(filterFlags.remote) != 0
			if err := ui.WriteSessionList(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout()), machines); err != nil {
				return apperr.Wrap(err)
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics, machines); err != nil {
				return apperr.Wrap(err)
			}
			if !report.Complete {
				return apperr.Wrap(errors.New("session inventory is incomplete"))
			}
			return nil
		},
	}
	registerSessionFilterFlags(command, &filterFlags)
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
