// Package doctor verifies that external tool configuration agrees with
// esheep's targets.
package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/jmcampanini/esheep/internal/config"
)

// Status is the outcome of one environment check.
type Status string

// Check outcomes.
const (
	StatusFail    Status = "fail"
	StatusPass    Status = "pass"
	StatusSkipped Status = "skipped"
)

// Check is one named environment verification and its outcome.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report contains every executed environment check.
type Report struct {
	Checks []Check
}

// Healthy reports whether no check failed.
func (report Report) Healthy() bool {
	for _, check := range report.Checks {
		if check.Status == StatusFail {
			return false
		}
	}
	return true
}

// Run executes every environment check against the loaded configuration.
// home is the absolute home directory external tool paths resolve under.
func Run(loaded config.LoadResult, home string) Report {
	return Report{Checks: []Check{
		piExclusion(loaded, filepath.Join(home, ".pi", "agent", "settings.json")),
	}}
}

const piExclusionName = "pi-skills-exclusion"

// piExclusion requires pi's global settings to exclude the codex target
// directory from pi's skill discovery, so skills installed for Codex are
// never loaded by pi. Only the exact expected entry counts: equivalent
// hand-written globs are rejected because pi's pattern matching does not
// expand '~' and treats hidden path segments specially.
func piExclusion(loaded config.LoadResult, settingsPath string) Check {
	if !loaded.Config.Targets.Pi.Enabled {
		return Check{Name: piExclusionName, Status: StatusSkipped, Detail: "pi target is disabled"}
	}

	expected := "!" + loaded.ResolvedTargets.Codex + "/**"
	fix := fmt.Sprintf("add %q to the \"skills\" array in %s", expected, settingsPath)
	data, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return Check{Name: piExclusionName, Status: StatusFail, Detail: "pi settings file is missing; " + fix}
	}
	if err != nil {
		return Check{Name: piExclusionName, Status: StatusFail, Detail: fmt.Sprintf("read pi settings: %v", err)}
	}

	var settings struct {
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return Check{Name: piExclusionName, Status: StatusFail, Detail: fmt.Sprintf("parse %s: %v; %s", settingsPath, err, fix)}
	}
	if !slices.Contains(settings.Skills, expected) {
		return Check{Name: piExclusionName, Status: StatusFail, Detail: "pi reads skills from the codex target directory; " + fix}
	}
	return Check{
		Name:   piExclusionName,
		Status: StatusPass,
		Detail: fmt.Sprintf("%s excludes %s from pi skill discovery", settingsPath, loaded.ResolvedTargets.Codex),
	}
}
