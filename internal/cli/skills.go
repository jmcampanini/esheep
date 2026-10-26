package cli

import (
	"github.com/spf13/cobra"
)

func newSkillsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "skills",
		Short: "Inspect known skills and deployment status",
		Long: `Inspect the skills found in configured sources and how they are deployed
on enabled targets, without changing sources or targets.

Commands in this group read the skills/ and agents-md/ directories of
each configured source and, when they report deployment health, the
skills_path and agents_md_path of each enabled target; comparing a
deployed skill renders it into a temporary .esheep-inspect-* directory
under the system temporary directory, deleted before the command ends.
Each command writes a report to stdout and diagnostics to stderr, never
prompts, and replaces the report with one JSON document under --json.
Run 'esheep sync' to change a target.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newSkillsListCommand(load, operations.list),
		newSkillsStatusCommand(load, operations.status),
	)
	return command
}
