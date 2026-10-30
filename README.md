# esheep

esheep finds historical sessions from Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations by reading their transcripts in place and read-only. It also records memories for the harness session in progress. esheep writes only under its memory root and contacts another machine only when `--remote` names one.


## Install

esheep ships versioned releases for macOS and Linux on amd64 and arm64 as prebuilt archives and a Homebrew cask, with a HEAD formula and source builds also available.

Install and upgrade with [mise](https://mise.jdx.dev):

```sh
mise use -g github:jmcampanini/esheep
mise upgrade github:jmcampanini/esheep
```

Install and upgrade the shared cask with Homebrew, which removes macOS quarantine before generating shell completions:

```sh
brew install --cask jmcampanini/tap/esheep
brew upgrade --cask jmcampanini/tap/esheep
```

Download your OS and architecture's archive from [releases](https://github.com/jmcampanini/esheep/releases), which include SHA-256 checksums and build provenance attestations, then extract it with `tar` and install or replace the binary:

```sh
tar -xzf <downloaded-archive>.tar.gz # Contains esheep, README.md, and LICENSE.
# macOS only: remove quarantine from a browser-downloaded binary before running it.
xattr -d com.apple.quarantine esheep
# Copy ./esheep to a directory on your PATH, replacing it when upgrading.
```

Install the HEAD formula with Homebrew and upgrade to the latest source commit:

```sh
brew tap jmcampanini/esheep https://github.com/jmcampanini/esheep
brew install --HEAD jmcampanini/esheep/esheep
brew upgrade --fetch-HEAD jmcampanini/esheep/esheep
```

Build from source with Git and mise, then copy the binary onto your `PATH`:

```sh
git clone https://github.com/jmcampanini/esheep
cd esheep
mise trust
mise install
mise run build
# Copy ./build/esheep to a directory on your PATH.
```


## Representative commands

| Command | Result |
|---|---|
| `esheep sessions list [--json]` | List historical harness sessions with their canonical transcript paths. |
| `esheep sessions search [pattern] [--json]` | Search session transcripts in place; hits address transcript lines. |
| `esheep sessions query` | Answer a list or search request read from stdin; the endpoint `--remote` runs over ssh. |
| `esheep memory record TEXT` | Append one memory for the current harness session under the memory root. |

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
