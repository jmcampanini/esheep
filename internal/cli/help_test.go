package cli

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/spf13/cobra"
)

const helpColumns = 80

func TestEveryCommandHelpMeetsTheBaseline(t *testing.T) {
	root := newRootCommand(config.Load)

	var assertHelp func(*cobra.Command)
	assertHelp = func(command *cobra.Command) {
		path := command.CommandPath()
		if command.Short == "" {
			t.Errorf("%s: no short help", path)
		}
		if command.Long == "" {
			t.Errorf("%s: no long description", path)
		}

		operands := strings.Fields(path)[1:]
		code, help, stderr := runCommand(t, loadNeverCalled(t), append(operands, "--help")...)
		if code != 0 || stderr != "" {
			t.Errorf("%s --help: exit %d, stderr = %q", path, code, stderr)
		}
		for _, line := range strings.Split(help, "\n") {
			if width := utf8.RuneCountInString(line); width > helpColumns {
				t.Errorf("%s --help: line is %d columns, over %d: %q", path, width, helpColumns, line)
			}
		}

		if isHelpTopic(command) {
			_, direct, _ := runCommand(t, loadNeverCalled(t), operands...)
			_, viaHelp, _ := runCommand(t, loadNeverCalled(t), append([]string{"help"}, operands...)...)
			if direct != help {
				t.Errorf("%s: direct invocation prints text other than its help:\n%s", path, direct)
			}
			if viaHelp != help {
				t.Errorf("help %s: prints text other than the topic's help:\n%s", strings.Join(operands, " "), viaHelp)
			}
		}

		for _, child := range command.Commands() {
			assertHelp(child)
		}
	}

	assertHelp(root)
}

func TestHelpRejectsUnknownOperands(t *testing.T) {
	for _, operands := range [][]string{{"missing"}, {"sessions", "missing"}, {"sessions", "list", "extra"}, {"exit-codes", "extra"}} {
		t.Run(strings.Join(operands, "/"), func(t *testing.T) {
			code, stdout, stderr := runCommand(t, loadNeverCalled(t), append([]string{"help"}, operands...)...)

			if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "esheep: unknown help topic ") || strings.Count(stderr, "\n") != 1 {
				t.Fatalf("help result: exit=%d stdout=%q stderr=%q, want one usage diagnostic", code, stdout, stderr)
			}
		})
	}
}

// loadNeverCalled fails the test if a command loads configuration while
// printing help.
func loadNeverCalled(t *testing.T) configLoader {
	t.Helper()
	return func(config.LoadOptions) (config.LoadResult, error) {
		t.Error("help loaded configuration")
		return config.LoadResult{}, errors.New("help loaded configuration")
	}
}
