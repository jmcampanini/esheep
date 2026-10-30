package cli

import (
	"errors"
	"time"

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
modification time when unavailable), archive state, recorded origin, project
directories, title where the grammar records one, and the canonical transcript
path.
Subagent and sidechain transcripts are excluded unless --subagents is
set. --since and --until keep sessions with at least one recorded event
in the period. Best-effort fields a grammar does not record appear as -.

The command exits nonzero when filesystem failures prevent a complete
inventory; a missing session root merely skips that location with a
diagnostic.

` + sessionHarnessHelp + `

` + sessionIDHelp + `

` + ui.StreamContractHelp + `

` + ui.ColorContractHelp + `

` + sessionPeriodHelp + `

` + ui.JSONContractHelp + `

` + sessionJSONHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
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
			report := operations.sessionList(command.Context(), sessionRoots(loaded), filter)

			if jsonOutput {
				if err := ui.WriteSessionListJSON(command.OutOrStdout(), report); err != nil {
					return err
				}
			} else if err := ui.WriteSessionList(command.OutOrStdout(), report, useColor(command, command.OutOrStdout())); err != nil {
				return err
			}
			if err := ui.WriteSessionDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return err
			}
			if !report.Complete {
				return errors.New("session inventory is incomplete")
			}
			return nil
		},
	}
	registerSessionFilterFlags(command, &filterFlags)
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON document")
	return command
}
