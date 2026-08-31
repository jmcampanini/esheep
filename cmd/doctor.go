package cmd

import (
	"errors"

	"github.com/jmcampanini/esheep/internal/doctor"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newDoctorCommand(load configLoader) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify external tool configuration agrees with esheep",
		Long: `Check that other tools on this machine are configured to cooperate with
esheep's targets, without changing anything.

The pi-skills-exclusion check requires pi's global settings file
(~/.pi/agent/settings.json) to keep pi from reading the shared Agent
Skills directory at ~/.agents/skills: the "skills" array must contain the
exact entry '!<absolute-home>/.agents/skills/**'. Pi discovers that directory
by default, so without the exclusion every skill installed there for Codex
is also loaded by pi. Only the
exact entry passes; equivalent hand-written patterns are rejected because
pi does not expand '~' inside patterns and treats hidden path segments
specially. The check is skipped while either the pi or codex target is disabled.

The report is payload on stdout, one row per check. The command exits 1
when any check fails, and each failure names the exact entry to add.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := doctor.Run(loaded)
			if err := ui.WriteDoctor(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if !report.Healthy() {
				return appError(errors.New("environment checks failed"))
			}
			return nil
		},
	}
}
