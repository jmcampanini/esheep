// Package discovery catalogs immediate child skills from configured sources.
package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmcampanini/esheep/internal/skill"
)

// Code identifies a stable discovery failure category.
type Code string

// Discovery diagnostic codes.
const (
	CodeSourceUnavailable Code = "source-unavailable"
	CodeSkillInvalid      Code = "skill-invalid"
	CodeCollision         Code = "collision"
)

// Source identifies one configured top-level collection.
type Source struct {
	Name string
	Path string
}

// Location identifies a discovered skill candidate.
type Location struct {
	Source       string
	RelativePath string
	Path         string
}

// Diagnostic describes one discovery or skill validation failure.
type Diagnostic struct {
	Code       Code
	Location   Location
	SkillCode  skill.Code
	Path       string
	Field      string
	Detail     string
	Err        error
	Collisions []Location
}

// Candidate is one immediate child with a present SKILL.md entry.
type Candidate struct {
	Location    Location
	Package     skill.Package
	Diagnostics []skill.Diagnostic
	Colliding   bool
}

// Valid reports whether the candidate may be rendered.
func (candidate Candidate) Valid() bool {
	return len(candidate.Diagnostics) == 0 && !candidate.Colliding
}

// Catalog contains stable-order candidates and aggregate diagnostics.
type Catalog struct {
	Candidates  []Candidate
	Diagnostics []Diagnostic
}

// ValidCandidates returns renderable candidates in discovery order.
func (catalog Catalog) ValidCandidates() []Candidate {
	valid := make([]Candidate, 0, len(catalog.Candidates))
	for _, candidate := range catalog.Candidates {
		if candidate.Valid() {
			valid = append(valid, candidate)
		}
	}
	return valid
}

// Discover inspects each source in input order and only its immediate,
// non-symlink child directories. It continues after structured failures.
func Discover(sources []Source) Catalog {
	var catalog Catalog
	for _, source := range sources {
		discoverSource(source, &catalog)
	}
	detectCollisions(&catalog)
	return catalog
}

func discoverSource(source Source, catalog *Catalog) {
	info, err := os.Stat(source.Path)
	if err != nil {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, err))
		return
	}
	if !info.IsDir() {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, fmt.Errorf("not a directory")))
		return
	}
	root, err := os.Open(source.Path)
	if err != nil {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, err))
		return
	}
	defer func() { _ = root.Close() }()
	info, err = root.Stat()
	if err != nil {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, err))
		return
	}
	if !info.IsDir() {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, fmt.Errorf("not a directory")))
		return
	}
	children, err := root.ReadDir(-1)
	if err != nil {
		catalog.Diagnostics = append(catalog.Diagnostics, sourceDiagnostic(source, err))
		return
	}
	sort.Slice(children, func(left, right int) bool { return children[left].Name() < children[right].Name() })
	for _, child := range children {
		name := child.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || !child.IsDir() || child.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidateRoot := filepath.Join(source.Path, name)
		if entries, readErr := os.ReadDir(candidateRoot); readErr == nil && !containsManifest(entries) {
			continue
		}
		loaded, loadErr := skill.Load(candidateRoot)
		location := Location{Source: source.Name, RelativePath: name, Path: candidateRoot}
		candidate := Candidate{Location: location, Package: loaded, Diagnostics: skill.ErrorDiagnostics(loadErr)}
		catalog.Candidates = append(catalog.Candidates, candidate)
		for _, diagnostic := range candidate.Diagnostics {
			catalog.Diagnostics = append(catalog.Diagnostics, Diagnostic{
				Code:      CodeSkillInvalid,
				Location:  location,
				SkillCode: diagnostic.Code,
				Path:      diagnostic.Path,
				Field:     diagnostic.Field,
				Detail:    diagnostic.Detail,
				Err:       diagnostic.Err,
			})
		}
	}
}

func sourceDiagnostic(source Source, err error) Diagnostic {
	return Diagnostic{Code: CodeSourceUnavailable, Location: Location{Source: source.Name, Path: source.Path}, Err: err}
}

// containsManifest reports whether any entry occupies the manifest namespace,
// including invalid profile variants that must surface as skill diagnostics.
func containsManifest(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if _, ok, err := skill.ParseManifestName(entry.Name()); ok || err != nil {
			return true
		}
	}
	return false
}

func detectCollisions(catalog *Catalog) {
	groups := make(map[string][]int)
	var order []string
	for index := range catalog.Candidates {
		candidate := &catalog.Candidates[index]
		name, ok := candidate.Package.Identity(candidate.Location.RelativePath)
		if !ok {
			continue
		}
		folded := strings.ToLower(name)
		if _, exists := groups[folded]; !exists {
			order = append(order, folded)
		}
		groups[folded] = append(groups[folded], index)
	}
	for _, folded := range order {
		indexes := groups[folded]
		if len(indexes) < 2 {
			continue
		}
		locations := make([]Location, 0, len(indexes))
		for _, index := range indexes {
			catalog.Candidates[index].Colliding = true
			locations = append(locations, catalog.Candidates[index].Location)
		}
		catalog.Diagnostics = append(catalog.Diagnostics, Diagnostic{
			Code:       CodeCollision,
			Location:   locations[0],
			Detail:     folded,
			Collisions: locations,
		})
	}
}
