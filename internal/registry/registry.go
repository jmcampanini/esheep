// Package registry manages the machine-owned repository registry.
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Registry errors support stable classification without pinning message text.
var (
	ErrMalformed       = errors.New("malformed repository registry")
	ErrDuplicateName   = errors.New("duplicate repository name")
	ErrDuplicateURL    = errors.New("duplicate repository URL")
	ErrSlugCollision   = errors.New("repository clone slug collision")
	ErrInvalidName     = errors.New("invalid repository name")
	ErrInvalidSource   = errors.New("invalid git source")
	ErrUnsafeClonePath = errors.New("unsafe clone path")
	ErrNotFound        = errors.New("repository not found")
)

// Repo is one entry in repos.toml. URL is retained exactly as supplied.
type Repo struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// Registry is ordered intentionally: repos are written in append order.
type Registry struct {
	Repos []Repo `toml:"repos"`
}

// Source describes the parts of a git source useful to the registry.
type Source struct {
	Canonical string
	Name      string
}

var namePart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var scpSource = regexp.MustCompile(`^(?:[^/@:\s]+@)?([^:/\s]+):(.+)$`)

// ParseSource validates a practical git source without contacting the network.
func ParseSource(raw string) (Source, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return Source{}, fmt.Errorf("%w: source must be a non-empty path or URL", ErrInvalidSource)
	}
	if m := scpSource.FindStringSubmatch(raw); m != nil && !filepath.IsAbs(raw) && !strings.Contains(raw, "://") {
		path := strings.Trim(m[2], "/")
		path = strings.TrimSuffix(path, ".git")
		if path == "" {
			return Source{}, fmt.Errorf("%w: source has no repository path", ErrInvalidSource)
		}
		host := strings.ToLower(m[1])
		name := host + "/" + path
		return Source{Canonical: "ssh://" + host + "/" + path, Name: name}, nil
	}

	u, err := url.Parse(raw)
	if err != nil && strings.Contains(raw, "://") {
		return Source{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	if err == nil && u.Scheme != "" {
		if u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				return Source{}, fmt.Errorf("%w: embedded URL passwords are not allowed", ErrInvalidSource)
			}
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "git", "ssh":
			if u.Hostname() == "" || u.User != nil && u.User.Username() == "" {
				return Source{}, fmt.Errorf("%w: URL has no host", ErrInvalidSource)
			}
			path := strings.Trim(u.EscapedPath(), "/")
			if unescaped, e := url.PathUnescape(path); e == nil {
				path = unescaped
			}
			path = strings.TrimSuffix(path, ".git")
			if path == "" {
				return Source{}, fmt.Errorf("%w: URL has no repository path", ErrInvalidSource)
			}
			name := strings.ToLower(u.Hostname()) + "/" + path
			canonical := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + "/" + path
			return Source{Canonical: canonical, Name: name}, nil
		case "file":
			if u.Host != "" && u.Host != "localhost" {
				return Source{}, fmt.Errorf("%w: file URL host is unsupported", ErrInvalidSource)
			}
			path := u.Path
			if path == "" {
				return Source{}, fmt.Errorf("%w: file URL has no path", ErrInvalidSource)
			}
			return localSource(path)
		default:
			return Source{}, fmt.Errorf("%w: unsupported URL scheme %q", ErrInvalidSource, u.Scheme)
		}
	}
	return localSource(raw)
}

func localSource(path string) (Source, error) {
	if path == "" {
		return Source{}, fmt.Errorf("%w: local source has no repository basename", ErrInvalidSource)
	}
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) || base == ".." {
		return Source{}, fmt.Errorf("%w: local source has no repository basename", ErrInvalidSource)
	}
	base = strings.TrimSuffix(base, ".git")
	if base == "" {
		return Source{}, fmt.Errorf("%w: invalid local repository basename", ErrInvalidSource)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return Source{}, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	return Source{Canonical: "file://" + filepath.ToSlash(abs), Name: base}, nil
}

// ValidateName validates a logical identifier and its slash-separated components.
func ValidateName(name string) error {
	if name == "" || len(name) > 255 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "\\") || strings.ContainsAny(name, "\x00\r\n\t") {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || !namePart.MatchString(part) {
			return fmt.Errorf("%w: %q", ErrInvalidName, name)
		}
	}
	return nil
}

// DeriveName returns the logical name implied by a git source.
func DeriveName(source string) (string, error) {
	parsed, err := ParseSource(source)
	if err != nil {
		return "", err
	}
	if err := ValidateName(parsed.Name); err != nil {
		return "", err
	}
	return parsed.Name, nil
}

// Slug returns the flat clone directory name for a logical identifier.
func Slug(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(name, "/", "-")
	if slug == "" || slug == "." || slug == ".." || slug[0] == '.' {
		return "", fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return slug, nil
}

// Load reads path. An absent registry is an empty registry; malformed content fails loudly.
func Load(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{}, nil
	}
	if err != nil {
		return Registry{}, err
	}
	return parse(data)
}

func parse(data []byte) (Registry, error) {
	var result Registry
	metadata, err := toml.Decode(string(data), &result)
	if err != nil {
		return Registry{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		return Registry{}, fmt.Errorf("%w: unknown key %q", ErrMalformed, undecoded[0].String())
	}
	if err := validateRegistry(result); err != nil {
		return Registry{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return result, nil
}

func save(path string, registry Registry) error {
	if err := validateRegistry(registry); err != nil {
		return err
	}
	var contents bytes.Buffer
	if err := toml.NewEncoder(&contents).Encode(registry); err != nil {
		return fmt.Errorf("encode repository registry: %w", err)
	}
	return atomicWrite(path, contents.Bytes())
}

func validateRegistry(registry Registry) error {
	names := make(map[string]struct{}, len(registry.Repos))
	urls := make(map[string]struct{}, len(registry.Repos))
	slugs := make(map[string]string, len(registry.Repos))
	for _, repo := range registry.Repos {
		if err := ValidateName(repo.Name); err != nil {
			return err
		}
		source, err := ParseSource(repo.URL)
		if err != nil {
			return err
		}
		if _, ok := names[repo.Name]; ok {
			return fmt.Errorf("%w: %q", ErrDuplicateName, repo.Name)
		}
		if _, ok := urls[source.Canonical]; ok {
			return fmt.Errorf("%w: %q", ErrDuplicateURL, repo.URL)
		}
		slug, err := Slug(repo.Name)
		if err != nil {
			return err
		}
		slugKey := strings.ToLower(slug)
		if prior, ok := slugs[slugKey]; ok {
			return fmt.Errorf("%w: %q conflicts with %q", ErrSlugCollision, repo.Name, prior)
		}
		names[repo.Name] = struct{}{}
		urls[source.Canonical] = struct{}{}
		slugs[slugKey] = repo.Name
	}
	return nil
}

// Add appends a repository, deriving its name when logicalName is omitted or empty.
func Add(path, source string, logicalName ...string) error {
	registry, err := Load(path)
	if err != nil {
		return err
	}
	parsed, err := ParseSource(source)
	if err != nil {
		return err
	}
	name := parsed.Name
	if len(logicalName) > 1 {
		return fmt.Errorf("%w: one logical name may be supplied", ErrInvalidName)
	}
	if len(logicalName) == 1 && logicalName[0] != "" {
		name = logicalName[0]
	}
	if err := ValidateName(name); err != nil {
		return err
	}
	registry.Repos = append(registry.Repos, Repo{Name: name, URL: source})
	return save(path, registry)
}

// List returns entries in their on-disk order.
func List(path string) ([]Repo, error) {
	registry, err := Load(path)
	if err != nil {
		return nil, err
	}
	return append([]Repo(nil), registry.Repos...), nil
}

// Remove deletes a registry entry and its clone directly beneath cloneRoot.
func Remove(path, name, cloneRoot string) error {
	registry, err := Load(path)
	if err != nil {
		return err
	}
	if err := ValidateName(name); err != nil {
		return err
	}
	index := -1
	for i, repo := range registry.Repos {
		if repo.Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	target, err := safeCloneRootPath(cloneRoot, name)
	if err != nil {
		return err
	}
	remaining := append([]Repo(nil), registry.Repos[:index]...)
	remaining = append(remaining, registry.Repos[index+1:]...)
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove clone: %w", err)
	}
	return save(path, Registry{Repos: remaining})
}

// ClonePath returns the expected clone path beneath cloneRoot without touching the filesystem.
func ClonePath(cloneRoot, name string) (string, error) { return safeCloneRootPath(cloneRoot, name) }

func safeCloneRootPath(cloneRoot, name string) (string, error) {
	if cloneRoot == "" {
		return "", fmt.Errorf("%w: empty clone root", ErrUnsafeClonePath)
	}
	root, err := filepath.Abs(cloneRoot)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnsafeClonePath, err)
	}
	if info, e := os.Lstat(root); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: clone root is a symlink", ErrUnsafeClonePath)
	}
	if resolved, e := filepath.EvalSymlinks(root); e == nil {
		root = resolved
	}
	slug, err := Slug(name)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, slug)
	if err := ensureDirectChild(root, target); err != nil {
		return "", err
	}
	return target, nil
}

func ensureDirectChild(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnsafeClonePath, err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnsafeClonePath, err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)) {
		return fmt.Errorf("%w: clone is not a direct child of root", ErrUnsafeClonePath)
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".repos.toml-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, e := os.Open(dir); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
