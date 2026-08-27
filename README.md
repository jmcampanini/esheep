# esheep

esheep manages Agent Skills from multiple Git repositories and renders them for Claude Code, Pi, Codex, and the shared Agent Skills directory.

Milestone 1 is being delivered incrementally. The current command surface manages configuration and the repository registry; synchronization arrives in the next chunk.

## Install or upgrade from HEAD

```sh
go install github.com/jmcampanini/esheep@main
```

The installed binary requires `git` for repository synchronization. Repository registration itself does not contact the network.

## Commands

```sh
esheep --version
esheep completion zsh
esheep config
esheep config --provenance
esheep repo add git@github.com:jmcampanini/skills.git
esheep repo add ../work-skills --name work/skills
esheep repo list
esheep repo remove work/skills
```

Commands never prompt. Payloads are written to stdout; diagnostics are written to stderr. Success exits zero and errors exit nonzero. Concurrent mutating esheep commands are unsupported in Milestone 1; run repository and synchronization mutations one at a time.

## Configuration

esheep loads settings in this order, with later sources taking precedence:

1. built-in defaults;
2. `$XDG_CONFIG_HOME/esheep/esheep.toml`, or `~/.config/esheep/esheep.toml`;
3. `ESHEEP_*` environment variables;
4. command-line flags.

`--config PATH` replaces automatic discovery and requires a loadable file. A relative `XDG_CONFIG_HOME` follows go-config-loader's current behavior and resolves from the process working directory.

```toml
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

Each target has matching flags such as `--claude-enabled=false` and `--claude-path PATH`, and environment variables such as `ESHEEP_CLAUDE_ENABLED` and `ESHEEP_CLAUDE_PATH`. Leading `~/` uses `HOME`; other relative target paths resolve from the process working directory.

`esheep config` emits redirectable TOML and comments showing resolved target, registry, and clone paths. `--provenance` adds the source of each setting. Milestone 1 has no sensitive settings; configuration rendering still passes through the redaction boundary used for future sensitive fields.

## Filesystem behavior

- Settings are human-owned. esheep reads but never creates or modifies `esheep.toml`.
- The machine-owned repository registry is `$XDG_STATE_HOME/esheep/repos.toml`, or `~/.local/state/esheep/repos.toml`. Only `repo add` and `repo remove` edit it.
- Repository clones live under `$XDG_DATA_HOME/esheep/repos/`, or `~/.local/share/esheep/repos/`.
- `repo remove` deletes only the registered repository's derived clone directory. Skill-install ownership and pruning arrive with the synchronization lifecycle chunk.
