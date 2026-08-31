package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
)

func TestRunVerifiesPiExclusion(t *testing.T) {
	t.Parallel()
	const expectedPlaceholder = "<expected>"
	codexRoot := "/custom/codex/skills"
	tests := []struct {
		name          string
		settings      string
		noFile        bool
		piDisabled    bool
		codexDisabled bool
		status        Status
	}{
		{name: "fixed agent skills entry passes with custom codex path", settings: `{"theme": "dark", "skills": ["extra/skill", "<expected>"]}`, status: StatusPass},
		{name: "missing settings file fails", noFile: true, status: StatusFail},
		{name: "absent skills array fails", settings: `{"theme": "dark"}`, status: StatusFail},
		{name: "equivalent tilde pattern fails", settings: `{"skills": ["!~/.agents/skills/**"]}`, status: StatusFail},
		{name: "old object-form skills fails", settings: `{"skills": {"customDirectories": []}}`, status: StatusFail},
		{name: "malformed settings fails", settings: `{"skills": [`, status: StatusFail},
		{name: "disabled pi target skips", noFile: true, piDisabled: true, status: StatusSkipped},
		{name: "disabled codex target skips", noFile: true, codexDisabled: true, status: StatusSkipped},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			expected := "!" + filepath.Join(home, ".agents", "skills") + "/**"
			if !test.noFile {
				settingsPath := filepath.Join(home, ".pi", "agent", "settings.json")
				if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
					t.Fatal(err)
				}
				settings := strings.ReplaceAll(test.settings, expectedPlaceholder, expected)
				if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			loaded := config.LoadResult{
				Config: config.Config{Targets: config.Targets{
					Codex: config.CodexTarget{Enabled: !test.codexDisabled},
					Pi:    config.PiTarget{Enabled: !test.piDisabled},
				}},
				Home:            home,
				ResolvedTargets: config.ResolvedTargets{Codex: codexRoot},
			}

			report := Run(loaded)

			if len(report.Checks) != 1 {
				t.Fatalf("Run() checks = %#v, want one", report.Checks)
			}
			check := report.Checks[0]
			if check.Name != "pi-skills-exclusion" || check.Status != test.status {
				t.Fatalf("Run() check = %#v, want %s %s", check, "pi-skills-exclusion", test.status)
			}
			if test.status == StatusFail && !strings.Contains(check.Detail, expected) {
				t.Errorf("failure detail %q does not name the entry to add", check.Detail)
			}
			if got, want := report.Healthy(), test.status != StatusFail; got != want {
				t.Errorf("Healthy() = %t, want %t", got, want)
			}
		})
	}
}
