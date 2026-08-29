package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
)

func TestMetadataCommandsDoNotLoadConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "help", args: []string{"--config", "missing.toml", "--help"}, want: "human-maintained local source directories"},
		{name: "version", args: []string{"--config", "missing.toml", "--version"}, want: "esheep version dev\n"},
		{name: "completion", args: []string{"--config", "missing.toml", "completion", "bash"}, want: "bash completion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			load := func(config.LoadOptions) (config.LoadResult, error) {
				calls++
				return config.LoadResult{}, errors.New("configuration was loaded")
			}
			code, stdout, stderr := runCommand(t, load, test.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr)
			}
			if calls != 0 {
				t.Fatalf("configuration loads = %d, want 0", calls)
			}
			if !strings.Contains(stdout, test.want) {
				t.Fatalf("stdout = %q, want content %q", stdout, test.want)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestInvalidOperandsDoNotLoadConfiguration(t *testing.T) {
	for _, args := range [][]string{{"config", "extra"}, {"completion"}, {"completion", "bash", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			load := func(config.LoadOptions) (config.LoadResult, error) {
				calls++
				return config.LoadResult{}, errors.New("configuration was loaded")
			}
			code, stdout, _ := runCommand(t, load, args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if calls != 0 {
				t.Fatalf("configuration loads = %d, want 0", calls)
			}
		})
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
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := newRootCommand(load)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	return execute(root, args), stdout.String(), stderr.String()
}
