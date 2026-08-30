# esheep

esheep manages Agent Skills from human-maintained source directories and renders them for Claude Code, Pi, Codex, and the shared Agent Skills directory. It never accesses the network, executes source content, or creates, updates, or deletes source directories.

Command help is the canonical reference: `esheep --help` and each command's `--help` describe every user-facing contract, `esheep help skill-format` describes the authoring format, and `esheep help exit-codes` describes exit statuses.

## Platform support

esheep supports macOS and Linux.

## Install

esheep distributes from HEAD only; there is no stable release channel.

### Homebrew

```sh
brew tap jmcampanini/esheep https://github.com/jmcampanini/esheep
brew install --HEAD jmcampanini/esheep/esheep
```

Upgrade to the latest commit:

```sh
brew upgrade --fetch-HEAD esheep
```

### From source

```sh
make build
# then copy ./build/esheep to a directory on your PATH
```

## Representative commands

| Command | Result |
|---|---|
| `esheep --version` | Print the build version. |
| `esheep completion zsh` | Write Zsh completion; bash, fish, and powershell work the same way. |
| `esheep config [--provenance]` | Write the effective configuration and resolved paths. |
| `esheep profiles [--json]` | Report effective and referenced profiles. |
| `esheep skills list [--json]` | Inventory skills in every configured source. |
| `esheep sync` | Install, repair, and prune esheep-owned output on enabled targets. |
| `esheep skills status [--json]` | Report source readiness and per-target deployment health. |

The typical loop after changing a source skill is `esheep sync` followed by `esheep skills status`.

## Configuration

Settings are discovered at `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `$HOME/.config/esheep/esheep.toml`; `--config PATH` replaces discovery. Source directories are configured only in the TOML file. Target enablement, paths, and active profiles can also come from `ESHEEP_*` variables and flags, with the full precedence documented in `esheep config --help`.

Profiles gate when a skill applies: a skill limited by an `esheep-only-profiles` frontmatter field or a `SKILL.<profile>.md` manifest variant installs only while one of its profiles is active. `esheep help skill-format` describes the format.

```toml
profiles = ["work"]
env_profiles = ["MACHINE_PROFILES"]

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

Users own the settings file and source directories and choose how both are maintained. esheep never creates, updates, or deletes either one.
