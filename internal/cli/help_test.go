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

func TestSearchHelpDocumentsClassificationAndRelationshipScope(t *testing.T) {
	help := newSessionsSearchCommand(nil, commandOperations{}).Long
	for _, field := range []string{`"kind"`, `"origin"`, `"call_id"`, `"parent_call_id"`, "multiple results", "never by adjacency", "relationship is unknown", "independently of each"} {
		if !strings.Contains(help, field) {
			t.Errorf("search help missing classification contract %q", field)
		}
	}
}

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

func TestRootHelpSeparatesTopicsFromCommands(t *testing.T) {
	code, help, stderr := runCommand(t, loadNeverCalled(t), "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	commands, topics, found := strings.Cut(help, "Help topics:\n")
	if !found || !strings.Contains(commands, "Commands:\n") || !strings.Contains(topics, "  exit-codes ") {
		t.Fatalf("root help does not group commands and topics:\n%s", help)
	}
	if strings.Contains(commands, "  exit-codes ") {
		t.Fatalf("exit-codes appears among commands:\n%s", commands)
	}
}

func TestHelpCompletesCommandsAndTopics(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{name: "root", args: []string{""}, want: []string{"sessions", "exit-codes", "help"}},
		{name: "group", args: []string{"sessions", ""}, want: []string{"list", "search"}},
		{name: "prefix", args: []string{"sessions", "se"}, want: []string{"search"}},
		{name: "unknown", args: []string{"unknown", ""}},
		{name: "leaf", args: []string{"sessions", "list", ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"__complete", "help"}, test.args...)
			code, stdout, stderr := runCommand(t, loadNeverCalled(t), args...)
			if code != 0 || !strings.HasSuffix(stdout, ":4\n") {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, want no file completion", code, stdout, stderr)
			}
			for _, name := range test.want {
				if !strings.Contains(stdout, name+"\t") {
					t.Errorf("stdout = %q, want completion for %s", stdout, name)
				}
			}
			if len(test.want) == 0 && stdout != ":4\n" {
				t.Errorf("stdout = %q, want no suggestions", stdout)
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
