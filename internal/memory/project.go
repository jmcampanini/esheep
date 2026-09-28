package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveProject identifies the project a working directory belongs to.
// Inside a git repository the identity is the origin remote as host/org/repo,
// so every clone and worktree converges on one project; a repository without
// a usable origin uses its main worktree root. Outside git the identity is
// the working directory itself. Both fallbacks are absolute paths.
func resolveProject(ctx context.Context, cwd string) (string, []string, error) {
	worktree, found := findWorktree(cwd)
	if !found {
		return filepath.Clean(cwd), nil, nil
	}

	commonDir, err := git(ctx, cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if errors.Is(err, exec.ErrNotFound) {
		return worktree, []string{"git is not installed; identifying the project by its worktree path"}, nil
	}
	if err != nil {
		return "", nil, err
	}
	root := filepath.Dir(commonDir)

	remote, err := git(ctx, cwd, "remote", "get-url", "origin")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 2 {
		return root, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	identity, ok := RemoteIdentity(remote)
	if !ok {
		return root, []string{fmt.Sprintf("origin %q is not a host/org/repo remote; identifying the project by its repository path", remote)}, nil
	}
	return identity, nil, nil
}

// findWorktree returns the nearest ancestor of dir, including dir itself,
// that contains a .git entry.
func findWorktree(dir string) (string, bool) {
	current := filepath.Clean(dir)
	for {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

// git runs one git command in dir and returns its trimmed stdout. A nonzero
// exit is returned as an *exec.ExitError wrapped with git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

var remoteSchemes = map[string]bool{"git": true, "git+ssh": true, "http": true, "https": true, "ssh": true, "ssh+git": true}

// RemoteIdentity converts a git remote URL into host/org/repo. It accepts
// ssh, git, http, and https URLs and scp-style user@host:org/repo forms,
// drops the scheme, user info, port, and .git suffix, and lowercases the
// host. Local paths and other forms report false.
func RemoteIdentity(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	var host, path string
	if scheme, rest, found := strings.Cut(remote, "://"); found {
		if !remoteSchemes[strings.ToLower(scheme)] {
			return "", false
		}
		parsed, err := url.Parse("x://" + rest)
		if err != nil {
			return "", false
		}
		host, path = parsed.Hostname(), parsed.Path
	} else {
		hostPart, pathPart, found := strings.Cut(remote, ":")
		if !found || strings.Contains(hostPart, "/") {
			return "", false
		}
		if _, after, hasUser := strings.Cut(hostPart, "@"); hasUser {
			hostPart = after
		}
		host, path = hostPart, pathPart
	}

	host = strings.ToLower(host)
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, "/")
	if host == "" || path == "" {
		return "", false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	return host + "/" + path, true
}
