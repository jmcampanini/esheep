# esheep

esheep manages Agent Skills from human-maintained source directories and renders them for Claude Code, Pi, Codex, and the shared Agent Skills directory. It never accesses the network and never creates, updates, or deletes source directories.

The command surface provides versioning, completion, effective-configuration reporting, source inventory, ownership-safe synchronization, and deployment health inspection.

## Platform support

esheep supports macOS and Linux.

## Install or upgrade from HEAD

```sh
go install github.com/jmcampanini/esheep@main
```

## Commands

| Command | Result |
|---|---|
| `esheep` or `esheep --help` | Show root help. |
| `esheep help [command]` or `esheep <command> --help` | Show command help. |
| `esheep --version` | Print the build version. |
| `esheep completion <bash\|zsh\|fish\|powershell>` | Write a shell completion script. |
| `esheep config [--provenance]` | Write the effective configuration and resolved paths. |
| `esheep skills` | Show help for skill inspection. |
| `esheep skills list [--json]` | Inventory skills in every configured source. |
| `esheep sync` | Reconcile enabled targets with configured sources. |
| `esheep skills status [--json]` | Report source readiness and per-target deployment health. |

`--config`, the target flags documented below, and `--help` are available on subcommands. Help, version, and completion do not load configuration. For example, `esheep completion zsh` writes Zsh completion. Commands accept only the arguments shown and never prompt.

Command payloads, help, and completion scripts go to stdout. Human-readable diagnostics and final error messages go to stderr; synchronization action rows and its summary remain payload on stdout. JSON inspection writes one complete document, including structured diagnostics, to stdout and does not duplicate an unsuccessful report as a stderr error. Human inspection and synchronization output use color only on terminals and honor `NO_COLOR`; redirected and JSON output contains no terminal escapes.

Exit status `0` means success. Exit status `1` means configuration, source input, target state, or output prevented a valid command from succeeding. Exit status `2` means invalid command usage or command wiring failure.

## Local-source workflow

1. Run `esheep config --provenance` to inspect the settings, precedence, and resolved paths without requiring source directories to exist.
2. Run `esheep skills list` to inspect each discovered skill's readiness without changing sources or targets. Use `--json` for automation.
3. Run `esheep sync` to install, repair, and prune esheep-owned output on enabled targets. The command continues unrelated work after individual skill or target failures and returns exit status `1` when its final summary contains failures.
4. Run `esheep skills status` as a deployment health check. Use `--json` for automation; drift, missing output, blocked destinations, invalid skills, and collisions make status unhealthy.

After changing a source, rerun `sync` and then `skills status`. Esheep reads only local settings and source content and never accesses the network.

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

The target prefixes are `claude`, `pi`, `codex`, and `agents`. Each has `--<target>-enabled=<bool>` and `--<target>-path PATH` flags plus matching `ESHEEP_<TARGET>_ENABLED` and `ESHEEP_<TARGET>_PATH` environment variables. For example, `--claude-enabled=false` overrides `ESHEEP_CLAUDE_ENABLED` and TOML for that invocation.

Source names are case-insensitively unique safe slash-separated identifiers. Source and target paths must be absolute, exactly `~`, or begin with `~/`; boundary rules still reject the home directory itself as a target. Source roots must be distinct and non-nested. Enabled target roots must also be distinct and non-nested, may not be symlinks, may not overlap a source root, and may not be `/` or the home directory. Users own the settings file and source directories and choose how both are maintained. Esheep never creates, updates, or deletes either one.

`esheep config` emits redirectable TOML followed by comments showing resolved configuration, source, and target paths. `--provenance` adds the source of each setting. The command reads but never creates or modifies `esheep.toml`. Invalid configuration or an explicitly selected file that cannot be loaded fails before any skill or target processing.

## Skill discovery and rendering

Each source is a read-only collection of top-level skill directories. Discovery inspects only immediate non-symlink child directories containing `SKILL.md`; the source root and grouping directories are not skills. Dot-directories and `node_modules` are skipped. Recognized skill contents are traversed to validate supporting files. Supporting paths must be unique under case-insensitive Unicode-normalized comparison, and absolute, escaping, cyclic, and directory symlinks are rejected. A missing, unreadable, or non-directory source makes source-consuming commands fail; help, version, completion, and `config` do not require sources to exist.

Skills use declarative YAML frontmatter and a Markdown body. Command hooks, `allowed-tools`, and policies that grant or broaden execution permissions are rejected. Every `claude`, `pi`, `codex`, or `agents` target block may disable that render. Claude and Pi also support the inert `argument-hint` field; Codex and agents support no additional metadata in Milestone 1.

Rendering is deterministic. Claude and Pi receive common fields and `argument-hint` when present; Codex and the shared target receive common fields only. Supporting files are rendered as non-executable data, and root `.esheep.toml` is reserved for ownership metadata.

`esheep skills list` inventories every discovered source skill. Readiness is `ready`, `invalid`, or `collision`; validation and collision diagnostics do not hide known entries. The command exits nonzero only when configuration or filesystem failures prevent complete discovery.

## Synchronization and status

`esheep sync` processes unrelated skills after individual failures and prints deterministic action rows followed by a summary. Every managed installation contains a strict marker:

```toml
source = "personal"
skill = "review-pr"
target = "claude"
```

Esheep owns only an installed directory whose regular `.esheep.toml` marker exactly matches its source, skill, and target. Synchronization never modifies an unmarked, mismatched, or symlinked destination. It stages replacements on the target filesystem, restores the prior directory when a swap fails before commit, detects skill and destination collisions case-insensitively, and prunes only validly marked stale output. Once the intended visible state is committed and verified, a transaction-cleanup failure preserves that state and returns a nonzero error naming the leftover transaction rather than attempting another rollback. Invalid, colliding, or unavailable configured source skills protect existing output because that output is not proven stale. Disabled targets remain entirely untouched. A blocked destination, validation failure, unavailable source, collision, installation failure, or pruning failure appears in the final report and makes `sync` exit `1` after unrelated work is attempted.

`esheep skills status` reports each ready skill as `synced`, `drifted`, `missing`, `disabled`, or `blocked` for every target. Invalid and colliding source skills have no target state. It independently inspects every enabled target even when no skills are discovered; missing and valid empty targets remain healthy. `blocked` means a destination or target cannot be inspected or managed safely. Status is a health check: it exits `0` only when every source skill is ready and every target is synced or disabled.

`--json` on `skills list` and `skills status` emits one complete document with structured diagnostics. List JSON includes `complete`, and status JSON includes `healthy`; both commands still use the process exit status to report failure.
