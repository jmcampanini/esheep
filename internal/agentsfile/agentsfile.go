// Package agentsfile selects and deploys the global agents file from source
// containers to harness destinations.
package agentsfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmcampanini/esheep/internal/naming"
)

// DirName is the container child directory that holds managed global agents
// files.
const DirName = "agents-md"

// BaseName is the unprofiled agents file name inside a container's agents
// directory.
const BaseName = "AGENTS.md"

// ParseName classifies a filename inside the agents directory. It reports
// the profile named by a variant agents file ("" for AGENTS.md), whether the
// name is an agents file at all, and an error when the name sits in the
// reserved AGENTS.<profile>.md namespace with an invalid profile segment.
func ParseName(name string) (string, bool, error) {
	if name == BaseName {
		return "", true, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 3 || parts[0] != "AGENTS" || parts[2] != "md" {
		return "", false, nil
	}
	if err := naming.ValidateProfileName(parts[1]); err != nil {
		return "", false, err
	}
	return parts[1], true, nil
}

// Source identifies one configured container to search.
type Source struct {
	Name string
	Path string
}

// Candidate is one discovered agents file.
type Candidate struct {
	Path    string
	Profile string
	Source  string
}

// FileName returns the candidate's name inside its agents directory.
func (candidate Candidate) FileName() string {
	return filepath.Base(candidate.Path)
}

// Diagnostic describes one discovery failure.
type Diagnostic struct {
	Err    error
	Path   string
	Source string
}

// Discover inspects the agents directory of each source container for
// agents files, following symlinks wherever they resolve. A container
// without an agents directory provides no agents files. Names in the
// reserved namespace that carry an invalid profile or do not resolve to a
// regular file are diagnostics, not candidates.
func Discover(sources []Source) ([]Candidate, []Diagnostic) {
	var candidates []Candidate
	var diagnostics []Diagnostic
	for _, source := range sources {
		directory := filepath.Join(source.Path, DirName)
		entries, err := readAgentsDir(directory)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Err: err, Path: directory, Source: source.Name})
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(directory, name)
			profile, ok, nameErr := ParseName(name)
			if nameErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Err: nameErr, Path: path, Source: source.Name})
				continue
			}
			if !ok {
				continue
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Err: statErr, Path: path, Source: source.Name})
				continue
			}
			if !info.Mode().IsRegular() {
				diagnostics = append(diagnostics, Diagnostic{Err: fmt.Errorf("not a regular file"), Path: path, Source: source.Name})
				continue
			}
			candidates = append(candidates, Candidate{Path: path, Profile: profile, Source: source.Name})
		}
	}
	return candidates, diagnostics
}

// readAgentsDir lists an agents directory. An absent path is not an error,
// while a present path that is unreadable, not a directory, or an
// unresolvable link is.
func readAgentsDir(directory string) ([]os.DirEntry, error) {
	info, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if _, lstatErr := os.Lstat(directory); errors.Is(lstatErr, os.ErrNotExist) {
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory")
	}

	return os.ReadDir(directory)
}

// Selection is the agents file chosen under the active profiles.
type Selection struct {
	Candidate Candidate
	Found     bool
}

// Select walks the active profiles in order and then the unprofiled tier.
// The first tier with exactly one candidate across all sources wins, an
// empty tier falls through, and a tier with several candidates is an error.
func Select(candidates []Candidate, profiles []string) (Selection, error) {
	tiers := append(append([]string(nil), profiles...), "")
	for _, tier := range tiers {
		var matches []Candidate
		for _, candidate := range candidates {
			if candidate.Profile == tier {
				matches = append(matches, candidate)
			}
		}
		if len(matches) > 1 {
			paths := make([]string, 0, len(matches))
			for _, match := range matches {
				paths = append(paths, match.Path)
			}
			return Selection{}, fmt.Errorf("multiple sources provide %s: %s", matches[0].FileName(), strings.Join(paths, ", "))
		}
		if len(matches) == 1 {
			return Selection{Candidate: matches[0], Found: true}, nil
		}
	}
	return Selection{}, nil
}

// State describes one deployed agents file.
type State string

// Deployment states.
const (
	StateBlocked  State = "blocked"
	StateDisabled State = "disabled"
	StateMissing  State = "missing"
	StateStale    State = "stale"
	StateSynced   State = "synced"
)

// Outcome describes one completed deployment.
type Outcome string

// Deployment outcomes.
const (
	OutcomeInstalled Outcome = "installed"
	OutcomeRepaired  Outcome = "repaired"
	OutcomeUnchanged Outcome = "unchanged"
)
