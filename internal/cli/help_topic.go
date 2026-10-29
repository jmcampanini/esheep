package cli

import "github.com/spf13/cobra"

// helpTopicAnnotation marks a command that holds documentation only, so
// the grammar and help tests can tell topics from commands that do work.
const helpTopicAnnotation = "esheep.help-topic"

// helpTopicContractHelp states the contract every help topic shares.
const helpTopicContractHelp = `This topic prints its text on stdout whether invoked directly or through
'esheep help'. It reads no files, changes nothing, never prompts, and has
no machine-readable form.`

// newHelpTopic builds a command that prints its help whether it is invoked
// directly or through 'help'.
func newHelpTopic(use, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Long:        long + "\n\n" + helpTopicContractHelp,
		GroupID:     "help-topics",
		Annotations: map[string]string{helpTopicAnnotation: "true"},
		Args:        cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
}

// isHelpTopic reports whether command was built by newHelpTopic.
func isHelpTopic(command *cobra.Command) bool {
	return command.Annotations[helpTopicAnnotation] == "true"
}
