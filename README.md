# esheep

esheep manages Agent Skills and a global agents file from human-maintained source directories and renders them for Claude Code, Pi, and Codex. The Codex target installs skills into the shared Agent Skills directory (`~/.agents/skills`) that Codex reads. esheep never executes source content or creates, updates, or deletes source directories, and it contacts another machine only when `--remote` names one.

esheep also finds historical harness sessions. `esheep sessions list` and `esheep sessions search` read the session transcripts Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations leave on disk, in place and read-only. Every result points at the canonical transcript file.

See `esheep sessions list --help` and `esheep sessions search --help` for session filters and remote queries.

See `esheep memory record --help` for memory recording.

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

With Git and [mise](https://mise.jdx.dev) installed (`brew install mise` on macOS), mise supplies the pinned Go toolchain from `mise.toml`:

```sh
git clone https://github.com/jmcampanini/esheep
cd esheep
mise trust
mise install
mise run build
# then copy ./build/esheep to a directory on your PATH
```

To upgrade, run `git pull`, rebuild with `mise install && mise run build`, and replace the binary on your `PATH`.

## Representative commands

| Command | Result |
|---|---|
| `esheep --version` | Print the build version. |
| `esheep completion zsh` | Write Zsh completion; bash, fish, and powershell work the same way. |
| `esheep config [--provenance]` | Write the effective configuration and resolved paths. |
| `esheep profiles [--json]` | Report effective and referenced profiles. |
| `esheep sessions list [--json]` | List historical harness sessions with their canonical transcript paths. |
| `esheep sessions search [pattern] [--json]` | Search session transcripts in place; hits address transcript lines. |
| `esheep sessions query` | Answer a list or search request read from stdin; the endpoint `--remote` runs over ssh. |
| `esheep skills list [--json]` | Inventory skills in every configured source. |
| `esheep sync` | Install, repair, and prune esheep-owned output on enabled targets. |
| `esheep skills status [--json]` | Report source readiness and per-target deployment health. |
| `esheep doctor` | Verify external tool configuration agrees with esheep. |
| `esheep memory record TEXT` | Append one memory for the current harness session under the memory root. |

The typical loop after changing a source skill is `esheep sync` followed by `esheep skills status`.

See `esheep help skill-format` for disabling source manifests.

Source skill manifests declare when to invoke the skill in `esheep-trigger`. esheep renders that text as `description` for each harness and reports it as `trigger` in list/status JSON. `esheep help skill-format` documents the source requirements.

## Configuration

Settings are discovered at `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `$HOME/.config/esheep/esheep.toml`; `--config PATH` replaces discovery. Source directories are configured only in the TOML file. Target enablement, paths, and active profiles can also come from `ESHEEP_*` variables and flags, with the full precedence documented in `esheep config --help`.

Each source is a container: skill directories live under `<source>/skills/`, and an optional global agents file lives under `<source>/agents-md/` as `AGENTS.md` or a profile variant `AGENTS.<profile>.md`. Files at the container root, including a repository-local `AGENTS.md`, are ignored, so a normal repository can be a source. The selected agents file is rendered for each enabled target's non-symlink `agents_md_path` (by default `~/.claude/CLAUDE.md`, `~/.pi/agent/AGENTS.md`, and `~/.codex/AGENTS.md`); ownership of those destinations is positional, so sync overwrites any existing regular file there and never deletes it.

Profiles gate when a skill applies: a skill limited by an `esheep-only-profiles` frontmatter field or a `SKILL.<profile>.md` manifest variant installs only while one of its profiles is active. Agents file selection walks the active profiles in the same spirit. `esheep help skill-format` describes the formats.

See `esheep help skill-format` for template includes in skills and agents files.

Claude, Pi, and Codex are enabled by default and have built-in skills, agents file, and session paths. A source is enough to get started:

```toml
[[sources]]
name = "personal"
path = "~/Code/skills"
```

Add target settings only to disable a target or override a path. For example:

```toml
[targets.pi]
enabled = false

[targets.codex]
agents_md_path = "~/custom-codex/AGENTS.md"
```

Omitted settings retain their defaults. `esheep config --help` lists the target paths; `esheep config` shows every effective setting, including defaults.

The Codex session home is selected by `--codex-home`, `ESHEEP_CODEX_HOME`, the TOML setting, `CODEX_HOME`, then `~/.codex`, in that order. Configuring one home determines both transcript locations.

Set `[sessions.claude-cowork].path` to override Cowork's macOS default or provide its required path on Linux.

Add a `[[machines]]` entry per other machine for `--remote`. `name` is its hostname; `host`, `command`, and `timeout` are optional and default to the name, `esheep`, and 60 seconds:

```toml
[[machines]]
name = "nas"

[[machines]]
name = "studio"
host = "javier@studio.tail1234.ts.net"
command = "/opt/homebrew/bin/esheep"
timeout = "2m"
```

Users own the settings file and source directories and choose how both are maintained. esheep never creates, updates, or deletes either one.
