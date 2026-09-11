# esheep

esheep manages Agent Skills and a global agents file from human-maintained source directories and renders them for Claude Code, Pi, and Codex. The Codex target installs skills into the shared Agent Skills directory (`~/.agents/skills`) that Codex reads. esheep never accesses the network, executes source content, or creates, updates, or deletes source directories.

esheep also finds historical harness sessions. `esheep sessions list` and `esheep sessions search` read the session transcripts Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations leave on disk, in place and read-only. Every result points at the canonical transcript file.

Use `esheep sessions list --id <id>` to locate a known session, or `esheep sessions search --id <id> 'pattern'` to search its content. IDs match exactly and case-sensitively; repeat `--id` or provide comma-separated IDs to select several sessions. Other filters still apply. Cowork accepts its full scoped ID or the complete `local_<id>` component and reports every matching scope.

Use `--harness chatgpt-work` to select identified local Work tasks, `--harness codex` for Codex CLI and desktop, or `--harness codex,chatgpt-work` for both. Unfiltered queries include all harnesses. Codex and Work share `[sessions.codex].home`, reading its `sessions/` and `archived_sessions/` directories. Both locations are included by default; use `--archive-state active` or `--archive-state archived` to select one. Work results require saved messages or supported tool records; partial history qualifies, while title-only and injected-context-only records do not. Results do not imply complete remote history.

Use `--harness claude-cowork` for locally saved Cowork conversations. On macOS, esheep discovers their `audit.jsonl` files under Claude's standard Application Support location. Linux requires a configured path. Missing companion metadata does not hide readable messages; conversations without an archive flag are treated as active. Primary requests to subagents and their returned results are searchable by default; `--subagents` adds child activity, including records embedded in a Cowork audit.

Every session reports a `projects` array. `--project` matches any recorded folder, including Cowork's selected host folders and Codex/Work workspace roots across saved turns. Folder filtering does not read the folders themselves.

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

Set `esheep-disabled: true` in a source manifest to retain and validate it without installing it. The next `esheep sync` removes its managed copies from enabled targets. The flag applies only to the selected manifest; profile variants do not inherit it from `SKILL.md`. Remove the flag or set it to `false` to re-enable that manifest. List and status report valid selected disabled manifests as `disabled`.

Source skill manifests declare when to invoke the skill in `esheep-trigger`. esheep renders that text as `description` for each harness and reports it as `trigger` in list/status JSON. `esheep help skill-format` documents the source requirements.

## Configuration

Settings are discovered at `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `$HOME/.config/esheep/esheep.toml`; `--config PATH` replaces discovery. Source directories are configured only in the TOML file. Target enablement, paths, and active profiles can also come from `ESHEEP_*` variables and flags, with the full precedence documented in `esheep config --help`.

Each source is a container: skill directories live under `<source>/skills/`, and an optional global agents file lives under `<source>/agents-md/` as `AGENTS.md` or a profile variant `AGENTS.<profile>.md`. Files at the container root, including a repository-local `AGENTS.md`, are ignored, so a normal repository can be a source. The selected agents file is rendered for each enabled target's non-symlink `agents_md_path` (by default `~/.claude/CLAUDE.md`, `~/.pi/agent/AGENTS.md`, and `~/.codex/AGENTS.md`); ownership of those destinations is positional, so sync overwrites any existing regular file there and never deletes it.

Profiles gate when a skill applies: a skill limited by an `esheep-only-profiles` frontmatter field or a `SKILL.<profile>.md` manifest variant installs only while one of its profiles is active. Agents file selection walks the active profiles in the same spirit. `esheep help skill-format` describes the formats.

Skills and agents files can include shared instructions or vary parts by harness. A whole-line `{{esheep.include "body"}}` inserts `esheep-body.md` from the skill directory or the source's `agents-md/` directory. Use `{{esheep.include-by-harness "body"}}` to select `esheep-body-pi.md`, `esheep-body-codex.md`, or `esheep-body-claude.md` instead. The corresponding `include-optional` and `include-by-harness-optional` forms insert zero bytes when the selected file is genuinely absent, without fallback. All four forms can nest together from the same owning root, with cycle detection and a two-level nesting limit. Agents files use the same variable language throughout, without interpreted frontmatter; content outside substitutions stays byte-identical. Include fragments remain source-only. See `esheep help skill-format` for the complete include contract.

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

Users own the settings file and source directories and choose how both are maintained. esheep never creates, updates, or deletes either one.
