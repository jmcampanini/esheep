package cmd

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

	var assertGrammar func(*cobra.Command)
	assertGrammar = func(command *cobra.Command) {
		if command.HasSubCommands() && command.RunE == nil {
			t.Errorf("%s: has subcommands but no RunE, so Cobra shows help before validating operands", command.CommandPath())
		}
		for _, child := range command.Commands() {
			// Cobra owns its help command; esheep declares its own completion command.
			if child.Name() == "help" {
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
	if stderr != "Error: cannot load settings\n" {
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
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommandWithOperations(load, operations)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	return execute(root, args), stdout.String(), stderr.String()
}
