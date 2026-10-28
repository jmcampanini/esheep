# esheep

esheep finds historical sessions from Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations by reading their transcripts in place and read-only. It also records memories for the harness session in progress. esheep writes only under its memory root and contacts another machine only when `--remote` names one.


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
| `esheep sessions list [--json]` | List historical harness sessions with their canonical transcript paths. |
| `esheep sessions search [pattern] [--json]` | Search session transcripts in place; hits address transcript lines. |
| `esheep sessions query` | Answer a list or search request read from stdin; the endpoint `--remote` runs over ssh. |
| `esheep memory record TEXT` | Append one memory for the current harness session under the memory root. |
| `esheep config [--provenance]` | Print the effective configuration and resolved paths. |

The canonical reference is `esheep --help`, each command's `--help`, and the help topic `esheep help exit-codes`.


## Required external programs

- `ssh`: `esheep sessions list --remote` and `esheep sessions search --remote` run it with `BatchMode=yes`, so the connection to each machine must already work without a prompt.
- `git` 2.31 or later: `esheep memory record` runs it to identify the project by its origin remote or main worktree root.


## Configuration

esheep discovers its settings at `$XDG_CONFIG_HOME/esheep/config.toml`, or `$HOME/.config/esheep/config.toml`. Every setting has a default, so esheep works without the file. `esheep config --help` describes the load order and the session, memory, and machine settings. A machine entry lets `--remote` search another machine's sessions:

```toml
[[machines]]
name = "nas"
```
