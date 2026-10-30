# esheep

esheep finds historical sessions from Claude Code, Pi, Codex, local ChatGPT Work tasks, and local Claude Cowork conversations by reading their transcripts in place and read-only. It writes nothing and never contacts another machine.


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

The canonical reference is `esheep --help`, each command's `--help`, and the help topic `esheep help exit-codes`.


## Configuration

esheep discovers its settings at `$XDG_CONFIG_HOME/esheep/config.toml`, or `$HOME/.config/esheep/config.toml`. Every setting has a default, so esheep works without the file. `esheep config --help` describes the load order and the session path settings:

```toml
[sessions.claude]
path = "~/.claude/projects"
```
