# esheep

esheep manages Agent Skills from human-maintained source directories and renders them for Claude Code, Pi, Codex, and the shared Agent Skills directory. It never accesses the network and never creates, updates, or deletes source directories.

Milestone 1 is delivered incrementally. The current command surface provides versioning, completion, and read-only effective-configuration reporting. Internal discovery, validation, and deterministic rendering are implemented and inspectable through tests; synchronization, CLI inspection, and linting arrive in later chunks.

## Platform support

esheep supports macOS and Linux.

## Install or upgrade from HEAD

```sh
go install github.com/jmcampanini/esheep@main
```

## Commands

```sh
esheep --version
esheep completion zsh
esheep config
esheep config --provenance
```

Commands never prompt. Payloads are written to stdout and diagnostics are written to stderr. Exit status `0` means success, `1` means a valid command failed because of application state or input, and `2` means invalid command usage or command wiring failure.

## Configuration

esheep loads settings in this order, with later sources taking precedence:

1. built-in target defaults;
2. `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `$HOME/.config/esheep/esheep.toml`;
3. `ESHEEP_*` target variables;
4. target flags.

`--config PATH` replaces automatic discovery and requires a loadable file. `HOME` and an explicitly supplied `XDG_CONFIG_HOME` must be absolute. Source directories are configured only in the human-owned TOML file.

```toml
[[sources]]
name = "personal"
path = "~/Code/skills"

[[sources]]
name = "work"
path = "~/Code/work-skills"

[targets.claude]
enabled = true
path = "~/.claude/skills"

[targets.pi]
enabled = true
path = "~/.pi/agent/skills"

[targets.codex]
enabled = true
path = "~/.codex/skills"

[targets.agents]
enabled = false
path = "~/.agents/skills"
```

Each target has matching flags such as `--claude-enabled=false` and `--claude-path PATH`, and environment variables such as `ESHEEP_CLAUDE_ENABLED` and `ESHEEP_CLAUDE_PATH`.

Source and target paths must be absolute, exactly `~`, or begin with `~/`; boundary rules still reject the home directory itself as a target. Source roots must be distinct and non-nested. Enabled target roots must also be distinct and non-nested, may not overlap a source root, and may not be `/` or the home directory. Esheep never creates, updates, or deletes source directories; users choose how those directories are maintained.

`esheep config` emits redirectable TOML followed by comments showing resolved configuration, source, and target paths. `--provenance` adds the source of each setting. The command reads but never creates or modifies `esheep.toml`.

## Skill discovery and rendering

Each source is a collection of top-level skill directories. Discovery inspects only immediate non-symlink child directories containing `SKILL.md`; the source root and grouping directories are not skills. Dot-directories and `node_modules` are skipped. Recognized skill contents are traversed to validate supporting files, including rejection of absolute, escaping, cyclic, and directory symlinks.

Skills use declarative YAML frontmatter and a Markdown body. Command hooks, `allowed-tools`, and policies that grant or broaden execution permissions are rejected. Every `claude`, `pi`, `codex`, or `agents` target block may disable that render. Claude and Pi also support the inert `argument-hint` field; Codex and agents support no additional metadata in Milestone 1.

Rendering is deterministic. Claude and Pi receive common fields and `argument-hint` when present; Codex and the shared target receive common fields only. Supporting files are rendered as non-executable data, and root `.esheep.toml` is reserved for ownership metadata. This capability is currently internal; the `sync`, `skills list`, and `lint` commands arrive in later chunks.

## Synchronization safety

When synchronization is added, every managed installation will be marked with its source, skill, and target identity. Synchronization never modifies an unmarked or mismatched directory, stages replacements on the target filesystem, rolls back failed swaps, detects collisions case-insensitively, and prunes only validly marked stale output. Disabled targets remain untouched.
