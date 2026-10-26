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

// BaseName is the base-layer agents file name inside a container's agents
// directory.
const BaseName = "AGENTS.md"

// ParseName classifies a filename inside the agents directory. It reports
// the layer named by a variant agents file ("" for the base AGENTS.md,
// "local" for AGENTS.local.md), whether the name is an agents file at all,
// and an error when the name sits in the reserved AGENTS.<layer>.md
// namespace with an invalid layer segment.
func ParseName(name string) (string, bool, error) {
	if name == BaseName {
		return "", true, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 3 || parts[0] != "AGENTS" || parts[2] != "md" {
		return "", false, nil
	}
	if err := naming.ValidateLayerSegment(parts[1]); err != nil {
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
	Path   string
	Layer  string
	Source string
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
// reserved namespace that carry an invalid layer or do not resolve to a
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
			layer, ok, nameErr := ParseName(name)
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
			candidates = append(candidates, Candidate{Path: path, Layer: layer, Source: source.Name})
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

// Selection is the agents file chosen under the effective layers.
type Selection struct {
	Candidate Candidate
	Found     bool
}

// Select walks the layer order: the base tier, the effective layers in
// order, then the local tier. The last tier with a candidate wins, an empty
// tier contributes nothing, and any tier in the order with several
// candidates across sources is an error even when a later tier would win.
// Candidates for layers outside the order are ignored.
func Select(candidates []Candidate, layers []string) (Selection, error) {
	var selection Selection
	for _, tier := range naming.LayerOrder(layers) {
		var matches []Candidate
		for _, candidate := range candidates {
			if candidate.Layer == tier {
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
			selection = Selection{Candidate: matches[0], Found: true}
		}
	}
	return selection, nil
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
