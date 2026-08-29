package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmcampanini/esheep/internal/render"
	"github.com/jmcampanini/esheep/internal/skill"
)

// State describes one target installation.
type State string

// Installation states.
const (
	StateBlocked  State = "blocked"
	StateDisabled State = "disabled"
	StateDrifted  State = "drifted"
	StateMissing  State = "missing"
	StateSynced   State = "synced"
)

// Action describes one synchronization result.
type Action string

// Synchronization actions.
const (
	ActionBlocked   Action = "blocked"
	ActionDisabled  Action = "disabled"
	ActionFailed    Action = "failed"
	ActionInstalled Action = "installed"
	ActionPruned    Action = "pruned"
	ActionRepaired  Action = "repaired"
	ActionUnchanged Action = "unchanged"
)

// Identity identifies one expected target installation.
type Identity struct {
	Skill  string
	Source string
	Target render.Target
}

// Request contains one target reconciliation request.
type Request struct {
	Identity Identity
	Package  skill.Package
	Root     string
}

// Result describes one completed or refused target operation.
type Result struct {
	Action   Action
	Detail   string
	Identity Identity
}

type filesystem struct {
	noReplace func(*targetRoot, string, string) error
	removeAll func(*os.Root, string) error
	rename    func(*os.Root, string, string) error
}

var defaultFilesystem = filesystem{
	noReplace: func(root *targetRoot, oldName, newName string) error { return root.renameNoReplace(oldName, newName) },
	removeAll: func(root *os.Root, name string) error { return root.RemoveAll(name) },
	rename:    func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) },
}

// Inspect compares an expected skill with its target installation without
// modifying the target.
func Inspect(ctx context.Context, request Request) (state State, resultErr error) {
	if err := validateRequest(request); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	disabled, err := render.Disabled(request.Package, request.Identity.Target)
	if err != nil {
		return "", err
	}
	if disabled {
		return StateDisabled, nil
	}

	root, exists, err := openTargetRoot(request.Root, false)
	if err != nil {
		return "", err
	}
	if !exists {
		return StateMissing, nil
	}
	defer func() { resultErr = errors.Join(resultErr, root.close()) }()

	destination, err := root.inspect(request.Identity)
	if err != nil || destination.state != StateSynced {
		return destination.state, err
	}
	staging, cleanup, err := renderExpected("", request)
	if err != nil {
		return "", err
	}
	if err := verifyDestination(root, request.Identity, destination); err != nil {
		return "", errors.Join(err, cleanup())
	}
	equal, compareErr := treesEqual(staging, destination.path)
	verificationErr := verifyDestination(root, request.Identity, destination)
	if err := errors.Join(compareErr, verificationErr, cleanup()); err != nil {
		return "", err
	}
	if !equal {
		return StateDrifted, nil
	}
	return StateSynced, nil
}

// Reconcile installs or repairs one enabled target skill.
func Reconcile(ctx context.Context, request Request) (Result, error) {
	return reconcile(ctx, request, defaultFilesystem)
}

func reconcile(ctx context.Context, request Request, fsys filesystem) (result Result, resultErr error) {
	result.Identity = request.Identity
	if err := validateRequest(request); err != nil {
		result.Action = ActionFailed
		return result, err
	}
	if err := ctx.Err(); err != nil {
		result.Action = ActionFailed
		return result, err
	}

	disabled, err := render.Disabled(request.Package, request.Identity.Target)
	if err != nil {
		result.Action = ActionFailed
		return result, err
	}
	if disabled {
		result.Action = ActionDisabled
		result.Detail = "skill disabled"
		return result, nil
	}

	root, _, err := openTargetRoot(request.Root, true)
	if err != nil {
		result.Action = ActionFailed
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.close()) }()

	destination, err := root.inspect(request.Identity)
	if err != nil {
		result.Action = ActionFailed
		return result, err
	}
	if destination.state == StateBlocked {
		result.Action = ActionBlocked
		result.Detail = "destination is not esheep-owned"
		return result, fmt.Errorf("install %s/%s for %s: %s", request.Identity.Source, request.Identity.Skill, request.Identity.Target, result.Detail)
	}

	transactionName, transactionPath, err := root.createTransaction(".esheep-txn-")
	if err != nil {
		result.Action = ActionFailed
		return result, err
	}
	stagingPath, err := renderInTransaction(transactionPath, request)
	if err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	stagingName := filepath.Join(transactionName, "staging")
	stagingInfo, err := verifyStaging(root, stagingName, stagingPath)
	if err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}

	if destination.state == StateMissing {
		return installFresh(result, root, stagingName, stagingInfo, transactionName, fsys)
	}
	if err := verifyDestination(root, request.Identity, destination); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	equal, err := treesEqual(stagingPath, destination.path)
	if err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := verifyDestination(root, request.Identity, destination); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if equal {
		result.Action = ActionUnchanged
		result.Detail = "already synchronized"
		return result, cleanupTransaction(fsys, root, transactionName)
	}
	return replaceOwned(result, root, destination, stagingName, stagingInfo, transactionName, fsys)
}

// Prune removes validly owned installations selected by stale.
func Prune(ctx context.Context, path string, target render.Target, stale func(Marker) bool) (results []Result, resultErr error) {
	if path == "" || !validTarget(target) || stale == nil {
		return nil, fmt.Errorf("prune target: invalid request")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, exists, err := openTargetRoot(path, false)
	if err != nil || !exists {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.close()) }()

	entries, err := ownedInstallations(root, target)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if !stale(entry.marker) {
			continue
		}
		result := Result{
			Action:   ActionPruned,
			Detail:   "stale owned installation",
			Identity: Identity{Skill: entry.marker.Skill, Source: entry.marker.Source, Target: target},
		}
		transactionName, _, err := root.createTransaction(".esheep-prune-")
		if err == nil {
			err = pruneOwned(root, entry, transactionName, defaultFilesystem)
		}
		if err != nil {
			result.Action = ActionFailed
			result.Detail = err.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

func validateRequest(request Request) error {
	if request.Root == "" || request.Package.Root == "" || request.Identity.Source == "" ||
		request.Identity.Skill == "" || !validTarget(request.Identity.Target) {
		return fmt.Errorf("install: invalid request")
	}
	if request.Package.Document.Name != request.Identity.Skill {
		return fmt.Errorf("install: package skill %q does not match identity %q", request.Package.Document.Name, request.Identity.Skill)
	}
	return nil
}

func renderExpected(parent string, request Request) (string, func() error, error) {
	transaction, err := os.MkdirTemp(parent, ".esheep-inspect-")
	if err != nil {
		return "", nil, fmt.Errorf("create inspection staging: %w", err)
	}
	staging, err := renderInTransaction(transaction, request)
	if err != nil {
		return "", nil, errors.Join(err, os.RemoveAll(transaction))
	}
	return staging, func() error {
		if err := os.RemoveAll(transaction); err != nil {
			return fmt.Errorf("clean inspection staging: %w", err)
		}
		return nil
	}, nil
}

func renderInTransaction(transaction string, request Request) (string, error) {
	staging := filepath.Join(transaction, "staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		return "", fmt.Errorf("create render staging: %w", err)
	}
	rendered, err := render.Render(staging, request.Package, request.Identity.Target)
	if err != nil {
		return "", fmt.Errorf("render target skill: %w", err)
	}
	if !rendered {
		return "", fmt.Errorf("render target skill: target is disabled")
	}
	marker, err := MarshalMarker(Marker{
		Skill:  request.Identity.Skill,
		Source: request.Identity.Source,
		Target: request.Identity.Target,
	})
	if err != nil {
		return "", err
	}
	markerPath := filepath.Join(staging, MarkerName)
	file, err := os.OpenFile(markerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create ownership marker: %w", err)
	}
	_, writeErr := file.Write(marker)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", fmt.Errorf("write ownership marker: %w", err)
	}
	if err := os.Chmod(markerPath, 0o644); err != nil {
		return "", fmt.Errorf("set ownership marker permissions: %w", err)
	}
	return staging, nil
}

func verifyStaging(root *targetRoot, name, path string) (os.FileInfo, error) {
	if err := root.verifyPath(); err != nil {
		return nil, err
	}
	info, err := root.handle.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("verify render staging: %w", err)
	}
	absoluteInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("verify render staging: %w", err)
	}
	if !os.SameFile(info, absoluteInfo) {
		return nil, fmt.Errorf("verify render staging: staging was replaced")
	}
	return info, nil
}

func verifyDestination(root *targetRoot, identity Identity, expected destination) error {
	if err := root.verifyPath(); err != nil {
		return err
	}
	if err := root.verifyEntry(identity.Skill, expected.info); err != nil {
		return err
	}
	absoluteInfo, err := os.Lstat(expected.path)
	if err != nil {
		return fmt.Errorf("verify destination: %w", err)
	}
	if !os.SameFile(expected.info, absoluteInfo) {
		return fmt.Errorf("verify destination: destination was replaced")
	}
	owned, err := root.ownedBy(identity)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("verify destination: ownership changed")
	}
	return nil
}

func installFresh(result Result, root *targetRoot, stagingName string, stagingInfo os.FileInfo, transactionName string, fsys filesystem) (Result, error) {
	result.Action = ActionInstalled
	result.Detail = "new installation"
	if err := root.verifyPath(); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(stagingName, stagingInfo); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if _, err := root.handle.Lstat(result.Identity.Skill); !errors.Is(err, os.ErrNotExist) {
		result.Action = ActionBlocked
		result.Detail = "destination appeared during installation"
		return result, errors.Join(fmt.Errorf("install staged skill: %s", result.Detail), cleanupTransaction(fsys, root, transactionName))
	}
	if err := fsys.noReplace(root, stagingName, result.Identity.Skill); err != nil {
		if errors.Is(err, os.ErrExist) {
			result.Action = ActionBlocked
			result.Detail = "destination appeared during installation"
		} else {
			result.Action = ActionFailed
			result.Detail = err.Error()
		}
		return result, errors.Join(fmt.Errorf("install staged skill: %w", err), cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(result.Identity.Skill, stagingInfo); err != nil {
		return result, err
	}
	if err := root.verifyPath(); err != nil {
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	return result, cleanupTransaction(fsys, root, transactionName)
}

func replaceOwned(result Result, root *targetRoot, installed destination, stagingName string, stagingInfo os.FileInfo, transactionName string, fsys filesystem) (Result, error) {
	backupName := filepath.Join(transactionName, "backup")
	if err := root.verifyPath(); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(result.Identity.Skill, installed.info); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(stagingName, stagingInfo); err != nil {
		result.Action = ActionFailed
		return result, errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := fsys.rename(root.handle, result.Identity.Skill, backupName); err != nil {
		result.Action = ActionFailed
		result.Detail = err.Error()
		return result, errors.Join(fmt.Errorf("backup installed skill: %w", err), cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(backupName, installed.info); err != nil {
		rollbackErr := fsys.noReplace(root, backupName, result.Identity.Skill)
		result.Action = ActionFailed
		return result, errors.Join(err, rollbackErr)
	}
	owned, ownershipErr := root.ownedAt(backupName, result.Identity)
	if ownershipErr != nil || !owned {
		rollbackErr := fsys.noReplace(root, backupName, result.Identity.Skill)
		result.Action = ActionFailed
		return result, errors.Join(fmt.Errorf("verify backed-up ownership: ownership changed"), ownershipErr, rollbackErr)
	}
	if err := root.verifyPath(); err != nil {
		rollbackErr := fsys.noReplace(root, backupName, result.Identity.Skill)
		result.Action = ActionFailed
		return result, errors.Join(err, rollbackErr)
	}
	if err := fsys.noReplace(root, stagingName, result.Identity.Skill); err != nil {
		rollbackErr := fsys.noReplace(root, backupName, result.Identity.Skill)
		result.Action = ActionFailed
		result.Detail = err.Error()
		if errors.Is(err, os.ErrExist) {
			result.Action = ActionBlocked
			result.Detail = "destination appeared during installation"
		}
		if rollbackErr != nil {
			return result, errors.Join(fmt.Errorf("install staged skill: %w", err), fmt.Errorf("restore installed skill: %w", rollbackErr))
		}
		return result, errors.Join(fmt.Errorf("install staged skill: %w", err), cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(result.Identity.Skill, stagingInfo); err != nil {
		result.Action = ActionFailed
		return result, rollbackCommitted(root, result.Identity.Skill, backupName, stagingInfo, transactionName, fsys, err)
	}
	if err := root.verifyPath(); err != nil {
		result.Action = ActionFailed
		return result, rollbackCommitted(root, result.Identity.Skill, backupName, stagingInfo, transactionName, fsys, err)
	}

	result.Action = ActionRepaired
	result.Detail = "drift repaired"
	return result, cleanupTransaction(fsys, root, transactionName)
}

func rollbackCommitted(root *targetRoot, skillName, backupName string, stagingInfo os.FileInfo, transactionName string, fsys filesystem, cause error) error {
	current, err := root.handle.Lstat(skillName)
	if errors.Is(err, os.ErrNotExist) {
		if restoreErr := fsys.noReplace(root, backupName, skillName); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("restore installed skill: %w", restoreErr))
		}
		return errors.Join(cause, cleanupTransaction(fsys, root, transactionName))
	}
	if err != nil || !os.SameFile(stagingInfo, current) {
		return errors.Join(cause, fmt.Errorf("restore installed skill: committed destination was replaced"), err)
	}
	failedName := filepath.Join(transactionName, "failed")
	if err := fsys.rename(root.handle, skillName, failedName); err != nil {
		return errors.Join(cause, fmt.Errorf("quarantine failed installation: %w", err))
	}
	if err := fsys.noReplace(root, backupName, skillName); err != nil {
		restoreErr := fmt.Errorf("restore installed skill: %w", err)
		if reinstateErr := fsys.noReplace(root, failedName, skillName); reinstateErr != nil {
			return errors.Join(cause, restoreErr, fmt.Errorf("reinstate failed installation: %w", reinstateErr))
		}
		return errors.Join(cause, restoreErr)
	}
	return errors.Join(cause, cleanupTransaction(fsys, root, transactionName))
}

func pruneOwned(root *targetRoot, entry ownedInstallation, transactionName string, fsys filesystem) error {
	backupName := filepath.Join(transactionName, "backup")
	if err := root.verifyPath(); err != nil {
		return errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(entry.name, entry.info); err != nil {
		return errors.Join(err, cleanupTransaction(fsys, root, transactionName))
	}
	if err := fsys.rename(root.handle, entry.name, backupName); err != nil {
		return errors.Join(fmt.Errorf("stage stale installation: %w", err), cleanupTransaction(fsys, root, transactionName))
	}
	if err := root.verifyEntry(backupName, entry.info); err != nil {
		rollbackErr := fsys.noReplace(root, backupName, entry.name)
		return errors.Join(err, rollbackErr)
	}
	identity := Identity{Skill: entry.marker.Skill, Source: entry.marker.Source, Target: entry.marker.Target}
	owned, ownershipErr := root.ownedAt(backupName, identity)
	if ownershipErr != nil || !owned {
		rollbackErr := fsys.noReplace(root, backupName, entry.name)
		return errors.Join(fmt.Errorf("verify staged ownership: ownership changed"), ownershipErr, rollbackErr)
	}
	if err := root.verifyPath(); err != nil {
		rollbackErr := fsys.noReplace(root, backupName, entry.name)
		return errors.Join(err, rollbackErr)
	}
	return cleanupTransaction(fsys, root, transactionName)
}

func cleanupTransaction(fsys filesystem, root *targetRoot, transactionName string) error {
	if err := fsys.removeAll(root.handle, transactionName); err != nil {
		return fmt.Errorf("clean target transaction %q: %w", filepath.Join(root.path, transactionName), err)
	}
	return nil
}

type ownedInstallation struct {
	info   os.FileInfo
	marker Marker
	name   string
}

func ownedInstallations(root *targetRoot, target render.Target) ([]ownedInstallation, error) {
	entries, err := root.readDir()
	if err != nil {
		return nil, err
	}
	var owned []ownedInstallation
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := root.handle.Lstat(entry.Name())
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		markerPath := filepath.Join(entry.Name(), MarkerName)
		markerInfo, err := root.handle.Lstat(markerPath)
		if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		data, same, err := readMarker(root.handle, markerPath, markerInfo)
		if err != nil || !same {
			continue
		}
		marker, err := ParseMarker(data)
		if err != nil || marker.Skill != entry.Name() || marker.Target != target {
			continue
		}
		owned = append(owned, ownedInstallation{info: info, marker: marker, name: entry.Name()})
	}
	return owned, nil
}
