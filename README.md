# esheep

esheep manages Agent Skills and a global agents file from human-maintained source directories and renders them for Claude Code, Pi, and Codex. It also finds historical sessions from Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations by reading their transcripts in place and read-only. esheep never executes source content or creates, updates, or deletes source directories. It contacts another machine only when `--remote` names one.


## Install

esheep supports macOS and Linux on amd64 and arm64. Versioned releases will provide prebuilt archives and a Homebrew cask in the shared tap. The first release has not been published; until then, use the HEAD formula or build from source.


### Versioned releases

After the first stable release, install and upgrade with [mise](https://mise.jdx.dev) or the shared Homebrew tap:

```sh
mise use -g github:jmcampanini/esheep
mise upgrade github:jmcampanini/esheep
brew install --cask jmcampanini/tap/esheep
brew upgrade --cask jmcampanini/tap/esheep
```

Alternatively, download the archive for your OS and architecture from the [releases page](https://github.com/jmcampanini/esheep/releases), extract it, and copy `esheep` onto your `PATH`. To upgrade, download the new archive and replace the binary. Archives include `README.md` and `LICENSE`; releases include SHA-256 checksums and GitHub build provenance attestations.

For a browser-downloaded archive on macOS, remove quarantine from the extracted binary with `xattr -d com.apple.quarantine esheep` before running it. The Homebrew cask handles this before generating bash, zsh, and fish completions.


### Homebrew HEAD alternative

The existing HEAD formula remains available for installation from the latest source commit:

```sh
brew tap jmcampanini/esheep https://github.com/jmcampanini/esheep
brew install --HEAD jmcampanini/esheep/esheep
```

Upgrade to the latest commit:

```sh
brew upgrade --fetch-HEAD jmcampanini/esheep/esheep
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


## Representative commands

| Command | Result |
|---|---|
| `esheep --version` | Print the build version. |
| `esheep completion zsh` | Write Zsh completion; bash, fish, and powershell work the same way. |
| `esheep layers [--json]` | Report effective and referenced layers. |
| `esheep sessions list [--json]` | List historical harness sessions with their canonical transcript paths. |
| `esheep sessions search [pattern] [--json]` | Search session transcripts in place; hits address transcript lines. |
| `esheep sessions query` | Answer a list or search request read from stdin; the endpoint `--remote` runs over ssh. |
| `esheep skills list [--json]` | Inventory skills in every configured source. |
| `esheep sync` | Install, repair, and prune esheep-owned output on enabled targets. |
| `esheep skills status [--json]` | Report source readiness and per-target deployment health. |
| `esheep doctor` | Verify external tool configuration agrees with esheep. |
| `esheep memory record TEXT` | Append one memory for the current harness session under the memory root. |

The canonical reference is `esheep --help`, each command's `--help`, and the help topics `esheep help skill-format` and `esheep help exit-codes`.

The typical loop after changing a source skill is `esheep sync` followed by `esheep skills status`.


## Required external programs

- `ssh`: `esheep sessions list --remote` and `esheep sessions search --remote` run it with `BatchMode=yes`, so the connection to each machine must already work without a prompt.
- `git` 2.31 or later: `esheep memory record` runs it to identify the project by its origin remote or main worktree root.


## Configuration

esheep discovers its settings at `$XDG_CONFIG_HOME/esheep/config.toml`, or `$HOME/.config/esheep/config.toml`. `esheep config --help` describes the load order and the target, memory, and machine settings. One source is enough to get started:

```toml
[[sources]]
name = "personal"
path = "~/Code/skills"
```
