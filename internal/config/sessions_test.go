package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestCodexHomePrecedenceAndDerivedLocations(t *testing.T) {
	for _, test := range []struct {
		codexHome  string
		esheepHome string
		flagHome   string
		name       string
		tomlHome   string
		want       string
		wantSource string
	}{
		{name: "default", want: "~/.codex", wantSource: "<default>"},
		{name: "empty Codex environment", codexHome: "", want: "~/.codex", wantSource: "<default>"},
		{name: "Codex environment", codexHome: "~/codex-env", want: "~/codex-env", wantSource: "CODEX_HOME"},
		{name: "TOML", codexHome: "~/codex-env", tomlHome: "~/configured", want: "~/configured", wantSource: "file"},
		{name: "Esheep environment", codexHome: "~/codex-env", tomlHome: "~/configured", esheepHome: "~/esheep-env", want: "~/esheep-env", wantSource: "<env>"},
		{name: "flag", codexHome: "~/codex-env", tomlHome: "~/configured", esheepHome: "~/esheep-env", flagHome: "~/flag", want: "~/flag", wantSource: "<pflag>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			env := testEnv(home, filepath.Join(home, "config"))
			if test.name != "default" {
				env["CODEX_HOME"] = test.codexHome
			}
			if test.esheepHome != "" {
				env["ESHEEP_CODEX_HOME"] = test.esheepHome
			}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.SetOutput(io.Discard)
			if err := RegisterFlags(flags); err != nil {
				t.Fatal(err)
			}
			if test.flagHome != "" {
				if err := flags.Parse([]string{"--codex-home", test.flagHome}); err != nil {
					t.Fatal(err)
				}
			}
			options := LoadOptions{Env: env, Flags: flags}
			if test.tomlHome != "" {
				options.ConfigPath = filepath.Join(home, "settings.toml")
				if err := os.WriteFile(options.ConfigPath, fmt.Appendf(nil, "[sessions.codex]\nhome = %q\n", test.tomlHome), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			result, err := Load(options)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := Render(result, ReportOptions{Provenance: true})
			if err != nil {
				t.Fatal(err)
			}

			wantHome := filepath.Join(home, strings.TrimPrefix(test.want, "~/"))
			if result.Config.Sessions.Codex.Home != test.want || result.ResolvedSessions.CodexHome != wantHome ||
				result.ResolvedSessions.Codex != filepath.Join(wantHome, "sessions") ||
				result.ResolvedSessions.CodexArchived != filepath.Join(wantHome, "archived_sessions") {
				t.Errorf("home = %q, resolved = %+v, want home %q", result.Config.Sessions.Codex.Home, result.ResolvedSessions, wantHome)
			}
			wantSource := test.wantSource
			if wantSource == "file" {
				wantSource = options.ConfigPath
			}
			if got := result.Report.Updates["sessions.codex.home"]; got != wantSource {
				t.Errorf("home provenance = %q, want %q", got, wantSource)
			}
			for _, want := range []string{wantHome, filepath.Join(wantHome, "sessions"), filepath.Join(wantHome, "archived_sessions"), "source: " + wantSource} {
				if !strings.Contains(string(rendered), want) {
					t.Errorf("config output missing %q: %s", want, rendered)
				}
			}
			if _, err := os.Stat(wantHome); !os.IsNotExist(err) {
				t.Errorf("Load created a Codex home: %v", err)
			}
		})
	}
}

func TestCodexDerivedLocationsResolveSymlinkAliases(t *testing.T) {
	base := t.TempDir()
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(canonical, "storage")
	if err := os.Mkdir(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sessions", "archived_sessions"} {
		if err := os.Symlink(archive, filepath.Join(base, name)); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}
	env := testEnv(base, filepath.Join(base, "config"))
	env["ESHEEP_CODEX_HOME"] = alias

	result, err := Load(LoadOptions{Env: env})

	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedSessions.CodexHome != canonical || result.ResolvedSessions.Codex != archive || result.ResolvedSessions.CodexArchived != archive {
		t.Errorf("resolved = %+v, want canonical home and shared storage %q", result.ResolvedSessions, archive)
	}
}
