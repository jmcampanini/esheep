# esheep

esheep manages Agent Skills and a global agents file from human-maintained source directories and renders them for Claude Code, Pi, and Codex. The Codex target installs skills into the shared Agent Skills directory (`~/.agents/skills`) that Codex reads. esheep never accesses the network, executes source content, or creates, updates, or deletes source directories.

esheep also finds historical harness sessions: `esheep sessions list` and `esheep sessions search` read the session transcripts Claude Code, Pi, and Codex leave on disk, in place and read-only, and point every result at the canonical transcript file.

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
| `esheep sessions list [--json]` | List historical harness sessions with their canonical transcript paths. |
| `esheep sessions search <pattern> [--json]` | Search session transcripts in place; hits address transcript lines. |
| `esheep skills list [--json]` | Inventory skills in every configured source. |
| `esheep sync` | Install, repair, and prune esheep-owned output on enabled targets. |
| `esheep skills status [--json]` | Report source readiness and per-target deployment health. |
| `esheep doctor` | Verify external tool configuration agrees with esheep. |

The typical loop after changing a source skill is `esheep sync` followed by `esheep skills status`.

## Configuration

Settings are discovered at `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `$HOME/.config/esheep/esheep.toml`; `--config PATH` replaces discovery. Source directories are configured only in the TOML file. Target enablement, paths, and active profiles can also come from `ESHEEP_*` variables and flags, with the full precedence documented in `esheep config --help`.

Each source is a container: skill directories live under `<source>/skills/`, and an optional global agents file lives under `<source>/agents-md/` as `AGENTS.md` or a profile variant `AGENTS.<profile>.md`. Files at the container root, including a repository-local `AGENTS.md`, are ignored, so a normal repository can be a source. The selected agents file is copied byte-identical to each enabled target's non-symlink `agents_md_path` (by default `~/.claude/CLAUDE.md`, `~/.pi/agent/AGENTS.md`, and `~/.codex/AGENTS.md`); ownership of those destinations is positional, so sync overwrites any existing regular file there and never deletes it.

Profiles gate when a skill applies: a skill limited by an `esheep-only-profiles` frontmatter field or a `SKILL.<profile>.md` manifest variant installs only while one of its profiles is active. Agents file selection walks the active profiles in the same spirit. `esheep help skill-format` describes the formats.

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
skills_path = "~/.claude/skills"
agents_md_path = "~/.claude/CLAUDE.md"

[targets.pi]
enabled = true
skills_path = "~/.pi/agent/skills"
agents_md_path = "~/.pi/agent/AGENTS.md"

[targets.codex]
enabled = true
skills_path = "~/.agents/skills"
agents_md_path = "~/.codex/AGENTS.md"

[sessions.claude]
path = "~/.claude/projects"

[sessions.pi]
path = "~/.pi/agent/sessions"

[sessions.codex]
path = "~/.codex/sessions"
```

Users own the settings file and source directories and choose how both are maintained. esheep never creates, updates, or deletes either one.
