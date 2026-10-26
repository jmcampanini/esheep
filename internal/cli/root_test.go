package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/spf13/cobra"
)

func TestCompletionDoesNotLoadConfiguration(t *testing.T) {
	calls := 0
	load := func(config.LoadOptions) (config.LoadResult, error) {
		calls++
		return config.LoadResult{}, errors.New("configuration was loaded")
	}

	code, stdout, stderr := runCommand(t, load, "--config", "missing.toml", "completion", "bash")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if calls != 0 {
		t.Fatalf("configuration loads = %d, want 0", calls)
	}
	if !strings.Contains(stdout, "bash completion") {
		t.Fatalf("stdout = %q, want bash completion script", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestEveryCommandDeclaresPositionalGrammar(t *testing.T) {
	root := newRootCommand(config.Load)

	if root.Args != nil {
		t.Error("root: Args is set, so an unknown subcommand is not rejected during command discovery")
	}
	if !root.DisableSuggestions {
		t.Error("root: DisableSuggestions is false")
	}
	for _, name := range []string{"help", "version"} {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("root: --%s is not registered at construction", name)
		}
	}

	var assertGrammar func(*cobra.Command)
	assertGrammar = func(command *cobra.Command) {
		if command != root && (command.HasSubCommands() || isHelpTopic(command)) {
			if command.RunE == nil {
				t.Errorf("%s: group or help topic has no RunE, so Cobra shows help before validating operands", command.CommandPath())
			}
			if command.Args != nil {
				if err := command.Args(command, nil); err != nil {
					t.Errorf("%s: group or help topic rejects a bare invocation: %v", command.CommandPath(), err)
				}
				if err := command.Args(command, []string{"unexpected"}); err == nil {
					t.Errorf("%s: group or help topic accepts an operand", command.CommandPath())
				}
			}
		}
		for _, child := range command.Commands() {
			// Cobra owns its help command and its hidden internals; esheep
			// declares its own completion command.
			if child.Name() == "help" || child.Hidden {
				continue
			}
			if child.Args == nil {
				t.Errorf("%s: no Args validator", child.CommandPath())
			}
			assertGrammar(child)
		}
	}

	assertGrammar(root)
}

func TestUsageErrorPrintsOneLineWithoutUsage(t *testing.T) {
	code, stdout, stderr := runCommand(t, successfulLoad, "skills", "list", "extra")

	if code != 2 || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	if !strings.HasPrefix(stderr, "esheep: ") || strings.Count(stderr, "\n") != 1 || strings.Contains(stderr, "Usage") {
		t.Fatalf("stderr = %q, want one 'esheep: <message>' line and no usage", stderr)
	}
}

func TestFailureLineFlattensMultilineMessages(t *testing.T) {
	load := func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{}, errors.New("cannot read settings\r\npermission denied\nretry")
	}

	code, stdout, stderr := runCommand(t, load, "config")

	if code != 1 || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	if want := "esheep: cannot read settings permission denied retry\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

func TestConfigurationFailureIsAnApplicationError(t *testing.T) {
	load := func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{}, errors.New("cannot load settings")
	}
	code, stdout, stderr := runCommand(t, load, "config")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "esheep: cannot load settings\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestEffectiveVersionUsesInjectedValue(t *testing.T) {
	prior := Version
	Version = "v1-test"
	t.Cleanup(func() { Version = prior })
	if got := effectiveVersion(); got != "v1-test" {
		t.Fatalf("effectiveVersion = %q", got)
	}
}

func runCommand(t *testing.T, load configLoader, args ...string) (int, string, string) {
	t.Helper()
	return runCommandWithOperations(t, load, commandOperations{
		list:   manage.List,
		status: manage.Status,
		sync:   manage.Sync,
	}, args...)
}

func runCommandWithOperations(t *testing.T, load configLoader, operations commandOperations, args ...string) (int, string, string) {
	t.Helper()
	return runCommandWithInput(t, load, operations, "", args...)
}

func runCommandWithInput(t *testing.T, load configLoader, operations commandOperations, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommandWithOperations(load, operations)
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	return execute(root, args), stdout.String(), stderr.String()
}
