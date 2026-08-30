// Package manage coordinates source inventory, target status, and synchronization.
package manage

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/discovery"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
)

// Readiness describes whether a known source skill can be synchronized.
type Readiness string

// Source readiness states.
const (
	ReadinessCollision Readiness = "collision"
	ReadinessInvalid   Readiness = "invalid"
	ReadinessReady     Readiness = "ready"
)

// Diagnostic is a stable source or target failure record.
type Diagnostic struct {
	Code    string `json:"code"`
	Detail  string `json:"detail,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
	Skill   string `json:"skill,omitempty"`
	Source  string `json:"source,omitempty"`
	Target  string `json:"target,omitempty"`
}

// KnownSkill is one discovered source skill.
type KnownSkill struct {
	Description string    `json:"description,omitempty"`
	Directory   string    `json:"directory"`
	Path        string    `json:"path"`
	Readiness   Readiness `json:"readiness"`
	Source      string    `json:"source"`
}

// ListReport is the complete known-skill inventory.
type ListReport struct {
	Complete    bool         `json:"complete"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Skills      []KnownSkill `json:"skills"`
}

// SkillStatus contains source readiness and available target states.
type SkillStatus struct {
	Description string                   `json:"description,omitempty"`
	Directory   string                   `json:"directory"`
	Path        string                   `json:"path"`
	Readiness   Readiness                `json:"readiness"`
	Source      string                   `json:"source"`
	Targets     map[string]install.State `json:"targets"`
}

// StatusReport is a deployment health report for known skills.
type StatusReport struct {
	Diagnostics []Diagnostic  `json:"diagnostics"`
	Healthy     bool          `json:"healthy"`
	Skills      []SkillStatus `json:"skills"`
}

// Summary counts synchronization outcomes.
type Summary struct {
	Blocked   int `json:"blocked"`
	Disabled  int `json:"disabled"`
	Failed    int `json:"failed"`
	Installed int `json:"installed"`
	Pruned    int `json:"pruned"`
	Repaired  int `json:"repaired"`
	Unchanged int `json:"unchanged"`
}

// SyncReport contains deterministic action rows and aggregate outcomes.
type SyncReport struct {
	Actions     []install.Result
	Diagnostics []Diagnostic
	Summary     Summary
}

type catalogResult struct {
	catalog     discovery.Catalog
	complete    bool
	diagnostics []Diagnostic
	skills      []KnownSkill
}

type targetSpec struct {
	enabled bool
	name    render.Target
	root    string
}

// List inventories every discovered source skill.
func List(ctx context.Context, loaded config.LoadResult) ListReport {
	catalog := buildCatalog(ctx, loaded)
	return ListReport{Complete: catalog.complete, Diagnostics: catalog.diagnostics, Skills: catalog.skills}
}

// Status inspects source readiness and per-target deployment state.
func Status(ctx context.Context, loaded config.LoadResult) StatusReport {
	catalog := buildCatalog(ctx, loaded)
	report := StatusReport{Diagnostics: catalog.diagnostics, Healthy: catalog.complete}
	targets := configuredTargets(loaded)
	for index, candidate := range catalog.catalog.Candidates {
		known := catalog.skills[index]
		row := SkillStatus{
			Description: known.Description,
			Directory:   known.Directory,
			Path:        known.Path,
			Readiness:   known.Readiness,
			Source:      known.Source,
			Targets:     make(map[string]install.State),
		}
		if known.Readiness != ReadinessReady {
			report.Healthy = false
			report.Skills = append(report.Skills, row)
			continue
		}

		for _, target := range targets {
			if !target.enabled {
				row.Targets[string(target.name)] = install.StateDisabled
				continue
			}
			state, err := install.Inspect(ctx, install.Request{
				Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				Package:  candidate.Package,
				Root:     target.root,
			})
			if err != nil {
				row.Targets[string(target.name)] = install.StateBlocked
				report.Diagnostics = append(report.Diagnostics, targetDiagnostic("target-inspection", known, target.name, err))
				report.Healthy = false
				continue
			}
			row.Targets[string(target.name)] = state
			if state != install.StateSynced && state != install.StateDisabled {
				report.Healthy = false
			}
		}
		report.Skills = append(report.Skills, row)
	}
	return report
}

// Sync synchronizes all valid source skills and prunes definitively stale output.
func Sync(ctx context.Context, loaded config.LoadResult) SyncReport {
	catalog := buildCatalog(ctx, loaded)
	report := SyncReport{Diagnostics: catalog.diagnostics}
	targets := configuredTargets(loaded)

	pruneStale(ctx, loaded, catalog, targets, &report)
	for index, candidate := range catalog.catalog.Candidates {
		known := catalog.skills[index]
		if known.Readiness != ReadinessReady {
			record(&report, install.Result{
				Action:   install.ActionFailed,
				Detail:   "source skill is " + string(known.Readiness),
				Identity: install.Identity{Skill: known.Directory, Source: known.Source},
			}, nil)
			continue
		}

		for _, target := range targets {
			if !target.enabled {
				record(&report, install.Result{
					Action:   install.ActionDisabled,
					Detail:   "target disabled",
					Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				}, nil)
				continue
			}
			result, err := install.Reconcile(ctx, install.Request{
				Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				Package:  candidate.Package,
				Root:     target.root,
			})
			record(&report, result, err)
			if err != nil {
				report.Diagnostics = append(report.Diagnostics, targetDiagnostic("synchronization", known, target.name, err))
			}
		}
	}
	for _, diagnostic := range catalog.catalog.Diagnostics {
		if diagnostic.Code != discovery.CodeSourceUnavailable {
			continue
		}
		record(&report, install.Result{
			Action:   install.ActionFailed,
			Detail:   "source is unavailable",
			Identity: install.Identity{Source: diagnostic.Location.Source},
		}, nil)
	}
	return report
}

func buildCatalog(ctx context.Context, loaded config.LoadResult) catalogResult {
	result := catalogResult{complete: ctx.Err() == nil}
	sources := make([]discovery.Source, 0, len(loaded.ResolvedSources))
	for _, source := range loaded.ResolvedSources {
		sources = append(sources, discovery.Source{Name: source.Name, Path: source.Path})
	}
	result.catalog = discovery.Discover(sources)
	for _, diagnostic := range result.catalog.Diagnostics {
		result.diagnostics = append(result.diagnostics, convertDiagnostic(diagnostic))
		if diagnostic.Code == discovery.CodeSourceUnavailable {
			result.complete = false
		}
	}
	for _, candidate := range result.catalog.Candidates {
		readiness := ReadinessReady
		switch {
		case len(candidate.Diagnostics) != 0:
			readiness = ReadinessInvalid
		case candidate.Colliding:
			readiness = ReadinessCollision
		}
		result.skills = append(result.skills, KnownSkill{
			Description: candidate.Package.Document.Description,
			Directory:   candidate.Location.RelativePath,
			Path:        candidate.Location.Path,
			Readiness:   readiness,
			Source:      candidate.Location.Source,
		})
	}
	return result
}

func configuredTargets(loaded config.LoadResult) []targetSpec {
	return []targetSpec{
		{enabled: loaded.Config.Targets.Claude.Enabled, name: render.TargetClaude, root: loaded.ResolvedTargets.Claude},
		{enabled: loaded.Config.Targets.Pi.Enabled, name: render.TargetPi, root: loaded.ResolvedTargets.Pi},
		{enabled: loaded.Config.Targets.Codex.Enabled, name: render.TargetCodex, root: loaded.ResolvedTargets.Codex},
		{enabled: loaded.Config.Targets.Agents.Enabled, name: render.TargetAgents, root: loaded.ResolvedTargets.Agents},
	}
}

func pruneStale(ctx context.Context, loaded config.LoadResult, catalog catalogResult, targets []targetSpec, report *SyncReport) {
	configured := make(map[string]struct{}, len(loaded.ResolvedSources))
	for _, source := range loaded.ResolvedSources {
		configured[source.Name] = struct{}{}
	}
	unavailable := make(map[string]struct{})
	for _, diagnostic := range catalog.catalog.Diagnostics {
		if diagnostic.Code == discovery.CodeSourceUnavailable {
			unavailable[diagnostic.Location.Source] = struct{}{}
		}
	}
	present := make(map[string]discovery.Candidate, len(catalog.catalog.Candidates))
	for _, candidate := range catalog.catalog.Candidates {
		present[candidateKey(candidate.Location.Source, candidate.Location.RelativePath)] = candidate
	}

	for _, target := range targets {
		if !target.enabled {
			continue
		}
		results, err := install.Prune(ctx, target.root, target.name, func(marker install.Marker) bool {
			if _, found := unavailable[marker.Source]; found {
				return false
			}
			if _, found := configured[marker.Source]; !found {
				return true
			}
			candidate, found := present[candidateKey(marker.Source, marker.Skill)]
			if !found {
				return true
			}
			if !candidate.Valid() {
				return false
			}
			disabled, disabledErr := render.Disabled(candidate.Package, target.name)
			return disabledErr == nil && disabled
		})
		for _, result := range results {
			record(report, result, nil)
			if result.Action == install.ActionFailed {
				report.Diagnostics = append(report.Diagnostics, Diagnostic{
					Code: "pruning", Message: result.Detail, Path: target.root,
					Skill: result.Identity.Skill, Source: result.Identity.Source, Target: string(target.name),
				})
			}
		}
		if err != nil {
			result := install.Result{
				Action:   install.ActionFailed,
				Detail:   err.Error(),
				Identity: install.Identity{Target: target.name},
			}
			record(report, result, nil)
			report.Diagnostics = append(report.Diagnostics, Diagnostic{
				Code: "pruning", Message: err.Error(), Path: target.root, Target: string(target.name),
			})
		}
	}
}

func candidateKey(source, skill string) string {
	return source + "\x00" + skill
}

func record(report *SyncReport, result install.Result, operationErr error) {
	report.Actions = append(report.Actions, result)
	switch result.Action {
	case install.ActionBlocked:
		report.Summary.Blocked++
		report.Summary.Failed++
	case install.ActionDisabled:
		report.Summary.Disabled++
	case install.ActionFailed:
		report.Summary.Failed++
	case install.ActionInstalled:
		report.Summary.Installed++
	case install.ActionPruned:
		report.Summary.Pruned++
	case install.ActionRepaired:
		report.Summary.Repaired++
	case install.ActionUnchanged:
		report.Summary.Unchanged++
	}
	if operationErr != nil && result.Action != install.ActionBlocked && result.Action != install.ActionFailed {
		report.Summary.Failed++
	}
}

func convertDiagnostic(diagnostic discovery.Diagnostic) Diagnostic {
	code := string(diagnostic.Code)
	if diagnostic.SkillCode != "" {
		code = string(diagnostic.SkillCode)
	}
	path := diagnostic.Location.Path
	if diagnostic.Path != "" && diagnostic.Path != "." {
		path = filepath.Join(path, filepath.FromSlash(diagnostic.Path))
	}
	result := Diagnostic{
		Code:   code,
		Detail: diagnostic.Detail,
		Field:  diagnostic.Field,
		Path:   path,
		Skill:  diagnostic.Location.RelativePath,
		Source: diagnostic.Location.Source,
	}
	if diagnostic.Err != nil {
		result.Message = diagnostic.Err.Error()
	}
	if diagnostic.Code == discovery.CodeCollision {
		locations := make([]string, 0, len(diagnostic.Collisions))
		for _, location := range diagnostic.Collisions {
			locations = append(locations, location.Source+"/"+location.RelativePath)
		}
		result.Message = "skill collides across " + strings.Join(locations, ", ")
	}
	return result
}

func targetDiagnostic(code string, known KnownSkill, target render.Target, err error) Diagnostic {
	return Diagnostic{
		Code: code, Message: fmt.Sprintf("%v", err), Path: known.Path,
		Skill: known.Directory, Source: known.Source, Target: string(target),
	}
}
