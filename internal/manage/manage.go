// Package manage coordinates source inventory, target status, and synchronization.
package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/discovery"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
	"github.com/jmcampanini/esheep/internal/skill"
)

// Readiness describes whether a known source skill can be synchronized.
type Readiness string

// Source readiness states.
const (
	ReadinessCollision Readiness = "collision"
	ReadinessConflict  Readiness = "conflict"
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

// KnownSkill is one discovered source skill. ProfileGate limits when the skill
// applies; an absent gate means every profile when HasManifest is true.
type KnownSkill struct {
	Description string    `json:"description,omitempty"`
	Directory   string    `json:"directory"`
	HasManifest bool      `json:"-"`
	Path        string    `json:"path"`
	ProfileGate []string  `json:"profile_gate,omitempty"`
	Readiness   Readiness `json:"readiness"`
	Source      string    `json:"source"`
}

// ListReport is the complete known-skill inventory.
type ListReport struct {
	Complete          bool         `json:"complete"`
	Diagnostics       []Diagnostic `json:"diagnostics"`
	EffectiveProfiles []string     `json:"effective_profiles"`
	Skills            []KnownSkill `json:"skills"`
}

// SkillStatus contains source readiness and available target states.
type SkillStatus struct {
	Description string                   `json:"description,omitempty"`
	Directory   string                   `json:"directory"`
	HasManifest bool                     `json:"-"`
	Path        string                   `json:"path"`
	ProfileGate []string                 `json:"profile_gate,omitempty"`
	Readiness   Readiness                `json:"readiness"`
	Source      string                   `json:"source"`
	Targets     map[string]install.State `json:"targets"`
}

// AgentsFileStatus reports the selected agents file and each target's
// deployed state.
type AgentsFileStatus struct {
	Path    string                      `json:"path"`
	Profile string                      `json:"profile,omitempty"`
	Source  string                      `json:"source"`
	Targets map[string]agentsfile.State `json:"targets"`
}

// StatusReport is a deployment health report for known skills and the
// selected agents file.
type StatusReport struct {
	AgentsFile        *AgentsFileStatus `json:"agents_file,omitempty"`
	Diagnostics       []Diagnostic      `json:"diagnostics"`
	EffectiveProfiles []string          `json:"effective_profiles"`
	Healthy           bool              `json:"healthy"`
	Skills            []SkillStatus     `json:"skills"`
}

// ProfilesReport describes the effective profile list and every valid profile
// referenced by discovered skills.
type ProfilesReport struct {
	Complete    bool         `json:"complete"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Effective   []string     `json:"effective"`
	Referenced  []string     `json:"referenced"`
}

// Summary counts synchronization outcomes.
type Summary struct {
	Blocked   int `json:"blocked"`
	Disabled  int `json:"disabled"`
	Failed    int `json:"failed"`
	Inactive  int `json:"inactive"`
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
	selections  []skill.Selection
	skills      []KnownSkill
}

type targetSpec struct {
	agentsMD string
	enabled  bool
	name     render.Target
	root     string
}

// List inventories every discovered source skill.
func List(ctx context.Context, loaded config.LoadResult) ListReport {
	catalog := buildCatalog(ctx, loaded)
	return ListReport{
		Complete:          catalog.complete,
		Diagnostics:       catalog.diagnostics,
		EffectiveProfiles: loaded.EffectiveProfiles,
		Skills:            catalog.skills,
	}
}

// Profiles reports the effective profile list and every valid profile
// referenced by discovered skills and agents file variants.
func Profiles(ctx context.Context, loaded config.LoadResult) ProfilesReport {
	catalog := buildCatalog(ctx, loaded)
	union := make(map[string]struct{})
	for _, candidate := range catalog.catalog.Candidates {
		for _, profile := range candidate.Package.ReferencedProfiles() {
			union[profile] = struct{}{}
		}
	}
	diagnostics := catalog.diagnostics
	if sourcesAvailable(catalog) {
		candidates, agentsDiagnostics := agentsfile.Discover(agentsSources(loaded))
		diagnostics = append(diagnostics, convertAgentsDiagnostics(agentsDiagnostics)...)
		for _, candidate := range candidates {
			if candidate.Profile != "" {
				union[candidate.Profile] = struct{}{}
			}
		}
	}
	referenced := make([]string, 0, len(union))
	for profile := range union {
		referenced = append(referenced, profile)
	}
	sort.Strings(referenced)
	return ProfilesReport{
		Complete:    catalog.complete,
		Diagnostics: diagnostics,
		Effective:   loaded.EffectiveProfiles,
		Referenced:  referenced,
	}
}

// Status inspects source readiness and per-target deployment state.
func Status(ctx context.Context, loaded config.LoadResult) StatusReport {
	catalog := buildCatalog(ctx, loaded)
	report := StatusReport{Diagnostics: catalog.diagnostics, EffectiveProfiles: loaded.EffectiveProfiles, Healthy: catalog.complete}
	targets := configuredTargets(loaded)
	blockedTargets, targetDiagnostics := inspectTargets(ctx, targets)
	report.Diagnostics = append(report.Diagnostics, targetDiagnostics...)
	if len(blockedTargets) != 0 {
		report.Healthy = false
	}

	for index, candidate := range catalog.catalog.Candidates {
		known := catalog.skills[index]
		selection := catalog.selections[index]
		row := SkillStatus{
			Description: known.Description,
			Directory:   known.Directory,
			HasManifest: known.HasManifest,
			Path:        known.Path,
			ProfileGate: known.ProfileGate,
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
			if _, blocked := blockedTargets[target.name]; blocked {
				row.Targets[string(target.name)] = install.StateBlocked
				continue
			}
			if !selection.Active {
				row.Targets[string(target.name)] = install.StateInactive
				continue
			}

			state, err := install.Inspect(ctx, install.Request{
				Document: selection.Manifest.Document,
				Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				Package:  candidate.Package,
				Profiles: loaded.EffectiveProfiles,
				Root:     target.root,
			})
			if err != nil {
				row.Targets[string(target.name)] = install.StateBlocked
				var targetRootErr *install.TargetRootError
				if errors.As(err, &targetRootErr) {
					diagnostic := Diagnostic{
						Code: "target-inspection", Message: fmt.Sprintf("%v", err), Path: targetRootErr.Path, Target: string(target.name),
					}
					blockedTargets[target.name] = diagnostic
					report.Diagnostics = append(report.Diagnostics, diagnostic)
				} else {
					report.Diagnostics = append(report.Diagnostics, targetDiagnostic("target-inspection", known, target, err))
				}
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
	statusAgentsFile(ctx, loaded, catalog, targets, &report)
	return report
}

// Sync synchronizes all valid source skills and prunes definitively stale output.
func Sync(ctx context.Context, loaded config.LoadResult) SyncReport {
	catalog := buildCatalog(ctx, loaded)
	report := SyncReport{Diagnostics: catalog.diagnostics}
	targets := configuredTargets(loaded)
	blockedTargets, targetDiagnostics := inspectTargets(ctx, targets)
	report.Diagnostics = append(report.Diagnostics, targetDiagnostics...)
	for _, target := range targets {
		diagnostic, blocked := blockedTargets[target.name]
		if !blocked {
			continue
		}
		record(&report, install.Result{
			Action:   install.ActionFailed,
			Detail:   diagnostic.Message,
			Identity: install.Identity{Target: target.name},
		}, nil)
	}

	pruneStale(ctx, loaded, catalog, targets, blockedTargets, &report)
	for index, candidate := range catalog.catalog.Candidates {
		known := catalog.skills[index]
		selection := catalog.selections[index]
		if known.Readiness != ReadinessReady {
			detail := "source skill is " + string(known.Readiness)
			if known.Readiness == ReadinessConflict {
				detail = "active profiles select multiple manifests"
			}
			record(&report, install.Result{
				Action:   install.ActionFailed,
				Detail:   detail,
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
			if _, blocked := blockedTargets[target.name]; blocked {
				continue
			}
			if !selection.Active {
				record(&report, install.Result{
					Action:   install.ActionInactive,
					Detail:   "profile not active",
					Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				}, nil)
				continue
			}

			result, err := install.Reconcile(ctx, install.Request{
				Document: selection.Manifest.Document,
				Identity: install.Identity{Skill: known.Directory, Source: known.Source, Target: target.name},
				Package:  candidate.Package,
				Profiles: loaded.EffectiveProfiles,
				Root:     target.root,
			})
			record(&report, result, err)
			if err == nil {
				continue
			}

			var targetRootErr *install.TargetRootError
			if errors.As(err, &targetRootErr) {
				diagnostic := Diagnostic{
					Code: "synchronization", Message: fmt.Sprintf("%v", err), Path: targetRootErr.Path, Target: string(target.name),
				}
				blockedTargets[target.name] = diagnostic
				report.Diagnostics = append(report.Diagnostics, diagnostic)
				continue
			}
			report.Diagnostics = append(report.Diagnostics, targetDiagnostic("synchronization", known, target, err))
		}
	}
	syncAgentsFile(ctx, loaded, catalog, targets, &report)
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
		selection := candidate.Package.Select(loaded.EffectiveProfiles)
		result.selections = append(result.selections, selection)
		readiness := ReadinessReady
		switch {
		case len(candidate.Diagnostics) != 0:
			readiness = ReadinessInvalid
		case candidate.Colliding:
			readiness = ReadinessCollision
		case len(selection.Conflicts) != 0:
			readiness = ReadinessConflict
		}
		if readiness == ReadinessConflict {
			result.diagnostics = append(result.diagnostics, Diagnostic{
				Code:    "profile-conflict",
				Message: "active profiles select multiple manifests: " + strings.Join(selection.Conflicts, ", "),
				Path:    candidate.Location.Path,
				Skill:   candidate.Location.RelativePath,
				Source:  candidate.Location.Source,
			})
		}
		result.skills = append(result.skills, KnownSkill{
			Description: describe(candidate, selection),
			Directory:   candidate.Location.RelativePath,
			HasManifest: len(candidate.Package.Manifests) != 0,
			Path:        candidate.Location.Path,
			ProfileGate: candidate.Package.Gate(),
			Readiness:   readiness,
			Source:      candidate.Location.Source,
		})
	}
	return result
}

// describe prefers the selected manifest's description so reports reflect
// what would render under the active profiles.
func describe(candidate discovery.Candidate, selection skill.Selection) string {
	if selection.Active {
		return selection.Manifest.Document.Description
	}
	if len(candidate.Package.Manifests) != 0 {
		return candidate.Package.Manifests[0].Document.Description
	}
	return ""
}

func configuredTargets(loaded config.LoadResult) []targetSpec {
	return []targetSpec{
		{agentsMD: loaded.ResolvedTargets.Claude.AgentsMD, enabled: loaded.Config.Targets.Claude.Enabled, name: render.TargetClaude, root: loaded.ResolvedTargets.Claude.Skills},
		{agentsMD: loaded.ResolvedTargets.Pi.AgentsMD, enabled: loaded.Config.Targets.Pi.Enabled, name: render.TargetPi, root: loaded.ResolvedTargets.Pi.Skills},
		{agentsMD: loaded.ResolvedTargets.Codex.AgentsMD, enabled: loaded.Config.Targets.Codex.Enabled, name: render.TargetCodex, root: loaded.ResolvedTargets.Codex.Skills},
		{agentsMD: loaded.ResolvedTargets.Agents.AgentsMD, enabled: loaded.Config.Targets.Agents.Enabled, name: render.TargetAgents, root: loaded.ResolvedTargets.Agents.Skills},
	}
}

type agentsFilePlan struct {
	content     []byte
	diagnostics []Diagnostic
	failed      bool
	selection   agentsfile.Selection
}

// planAgentsFile selects the agents file for the active profiles. It skips
// selection entirely while any configured source is unavailable so a partial
// view can never pick the wrong file.
func planAgentsFile(loaded config.LoadResult, catalog catalogResult) agentsFilePlan {
	if !sourcesAvailable(catalog) {
		return agentsFilePlan{}
	}
	candidates, discoveryDiagnostics := agentsfile.Discover(agentsSources(loaded))
	plan := agentsFilePlan{diagnostics: convertAgentsDiagnostics(discoveryDiagnostics)}
	if len(plan.diagnostics) != 0 {
		plan.failed = true
		return plan
	}
	selection, err := agentsfile.Select(candidates, loaded.EffectiveProfiles)
	if err != nil {
		plan.failed = true
		plan.diagnostics = append(plan.diagnostics, Diagnostic{Code: "agents-file-selection", Message: err.Error()})
		return plan
	}
	plan.selection = selection
	if !selection.Found {
		return plan
	}
	content, err := os.ReadFile(selection.Candidate.Path)
	if err != nil {
		plan.failed = true
		plan.diagnostics = append(plan.diagnostics, Diagnostic{
			Code: "agents-file-selection", Message: err.Error(), Path: selection.Candidate.Path, Source: selection.Candidate.Source,
		})
		return plan
	}
	plan.content = content
	return plan
}

func syncAgentsFile(ctx context.Context, loaded config.LoadResult, catalog catalogResult, targets []targetSpec, report *SyncReport) {
	plan := planAgentsFile(loaded, catalog)
	report.Diagnostics = append(report.Diagnostics, plan.diagnostics...)
	if plan.failed {
		identity := install.Identity{Skill: agentsfile.BaseName}
		if plan.selection.Found {
			identity.Skill = plan.selection.Candidate.FileName()
			identity.Source = plan.selection.Candidate.Source
		}
		record(report, install.Result{Action: install.ActionFailed, Detail: "agents file selection failed", Identity: identity}, nil)
		return
	}
	if !plan.selection.Found {
		return
	}

	candidate := plan.selection.Candidate
	for _, target := range targets {
		identity := install.Identity{Skill: candidate.FileName(), Source: candidate.Source, Target: target.name}
		if !target.enabled {
			record(report, install.Result{Action: install.ActionDisabled, Detail: "target disabled", Identity: identity}, nil)
			continue
		}
		outcome, err := agentsfile.Deploy(ctx, plan.content, target.agentsMD)
		if err != nil {
			record(report, install.Result{Action: install.ActionFailed, Detail: err.Error(), Identity: identity}, nil)
			report.Diagnostics = append(report.Diagnostics, Diagnostic{
				Code: "synchronization", Message: fmt.Sprintf("%v", err), Path: target.agentsMD,
				Skill: identity.Skill, Source: identity.Source, Target: string(target.name),
			})
			continue
		}
		result := install.Result{Identity: identity}
		switch outcome {
		case agentsfile.OutcomeInstalled:
			result.Action = install.ActionInstalled
			result.Detail = "agents file installed"
		case agentsfile.OutcomeRepaired:
			result.Action = install.ActionRepaired
			result.Detail = "drift repaired"
		case agentsfile.OutcomeUnchanged:
			result.Action = install.ActionUnchanged
			result.Detail = "already synchronized"
		}
		record(report, result, nil)
	}
}

func statusAgentsFile(ctx context.Context, loaded config.LoadResult, catalog catalogResult, targets []targetSpec, report *StatusReport) {
	plan := planAgentsFile(loaded, catalog)
	report.Diagnostics = append(report.Diagnostics, plan.diagnostics...)
	if plan.failed {
		report.Healthy = false
		return
	}
	if !plan.selection.Found {
		return
	}

	candidate := plan.selection.Candidate
	row := &AgentsFileStatus{
		Path:    candidate.Path,
		Profile: candidate.Profile,
		Source:  candidate.Source,
		Targets: make(map[string]agentsfile.State),
	}
	for _, target := range targets {
		if !target.enabled {
			row.Targets[string(target.name)] = agentsfile.StateDisabled
			continue
		}
		state, err := agentsfile.Inspect(ctx, plan.content, target.agentsMD)
		if err != nil {
			row.Targets[string(target.name)] = agentsfile.StateBlocked
			report.Diagnostics = append(report.Diagnostics, Diagnostic{
				Code: "target-inspection", Message: fmt.Sprintf("%v", err), Path: target.agentsMD,
				Skill: candidate.FileName(), Source: candidate.Source, Target: string(target.name),
			})
			report.Healthy = false
			continue
		}
		row.Targets[string(target.name)] = state
		if state != agentsfile.StateSynced {
			report.Healthy = false
		}
	}
	report.AgentsFile = row
}

func sourcesAvailable(catalog catalogResult) bool {
	for _, diagnostic := range catalog.catalog.Diagnostics {
		if diagnostic.Code == discovery.CodeSourceUnavailable {
			return false
		}
	}
	return true
}

func agentsSources(loaded config.LoadResult) []agentsfile.Source {
	sources := make([]agentsfile.Source, 0, len(loaded.ResolvedSources))
	for _, source := range loaded.ResolvedSources {
		sources = append(sources, agentsfile.Source{Name: source.Name, Path: source.Path})
	}
	return sources
}

func convertAgentsDiagnostics(diagnostics []agentsfile.Diagnostic) []Diagnostic {
	converted := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		converted = append(converted, Diagnostic{
			Code: "agents-file-selection", Message: diagnostic.Err.Error(), Path: diagnostic.Path, Source: diagnostic.Source,
		})
	}
	return converted
}

func inspectTargets(ctx context.Context, targets []targetSpec) (map[render.Target]Diagnostic, []Diagnostic) {
	blocked := make(map[render.Target]Diagnostic)
	var diagnostics []Diagnostic
	for _, target := range targets {
		if !target.enabled {
			continue
		}
		if err := install.InspectTarget(ctx, target.root); err != nil {
			diagnostic := Diagnostic{
				Code: "target-inspection", Message: fmt.Sprintf("%v", err), Path: target.root, Target: string(target.name),
			}
			blocked[target.name] = diagnostic
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return blocked, diagnostics
}

func pruneStale(
	ctx context.Context,
	loaded config.LoadResult,
	catalog catalogResult,
	targets []targetSpec,
	blockedTargets map[render.Target]Diagnostic,
	report *SyncReport,
) {
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
	present := make(map[string]presentCandidate, len(catalog.catalog.Candidates))
	for index, candidate := range catalog.catalog.Candidates {
		key := candidateKey(candidate.Location.Source, candidate.Location.RelativePath)
		present[key] = presentCandidate{candidate: candidate, selection: catalog.selections[index]}
	}

	for _, target := range targets {
		if !target.enabled {
			continue
		}
		if _, blocked := blockedTargets[target.name]; blocked {
			continue
		}

		results, err := install.Prune(ctx, target.root, target.name, func(marker install.Marker) bool {
			if _, found := unavailable[marker.Source]; found {
				return false
			}
			if _, found := configured[marker.Source]; !found {
				return true
			}
			entry, found := present[candidateKey(marker.Source, marker.Skill)]
			if !found {
				return true
			}
			if !entry.candidate.Valid() || len(entry.selection.Conflicts) != 0 {
				return false
			}
			if !entry.selection.Active {
				return true
			}
			disabled, disabledErr := render.Disabled(entry.selection.Manifest.Document, target.name, loaded.EffectiveProfiles)
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

type presentCandidate struct {
	candidate discovery.Candidate
	selection skill.Selection
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
	case install.ActionInactive:
		report.Summary.Inactive++
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

func targetDiagnostic(code string, known KnownSkill, target targetSpec, err error) Diagnostic {
	return Diagnostic{
		Code: code, Message: fmt.Sprintf("%v", err), Path: filepath.Join(target.root, filepath.FromSlash(known.Directory)),
		Skill: known.Directory, Source: known.Source, Target: string(target.name),
	}
}
