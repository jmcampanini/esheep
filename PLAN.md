# Remote session search

Design and implementation plan for running `esheep sessions list` and `esheep sessions search` across several machines from one of them. Settled on 2026-09-13 through a pivotal-questions interview. Implementation has not started.

## Goal and constraints

- One command lists or searches sessions on the local machine plus any set of remote machines, merged into one report.
- The default stays local. No network traffic happens unless `--remote` names a machine.
- esheep is installed on every machine. The remote side is the same binary answering a query over ssh. Nothing else is required there.
- Each machine keeps its own config. The remote reads its own `esheep.toml`, so the local side never needs to know where a remote keeps transcripts.
- Read-only everywhere. Transcripts are read in place, and esheep keeps no cache or mirror between runs.
- esheep never prompts. ssh runs in batch mode, and a host-key or password prompt becomes an actionable error.
- Machines are a fleet that grows. Two today, more later, identified by hostname.

## Transport

esheep runs one `ssh` call per machine with a fixed command line. The remote esheep reads a JSON request on stdin, applies every filter against its own transcripts and config, and writes today's `--json` document on stdout. Only results cross the wire.

| Option | Verdict | Why |
|---|---|---|
| esheep on each remote over `ssh` | chosen | Results-only transfer, exact filters, remote config stays remote, `~/.ssh/config` handles keys, aliases, jump hosts, and Tailscale. No new Go dependencies. |
| `find \| tar` pull over `ssh` | rejected | Nothing to install remotely, but every query moves transcript bytes, needs a mandatory `--since`, and the local config must describe remote paths. |
| SFTP over `ssh -s` | rejected | Lazy reads, but still moves transcript bytes and adds an SFTP client library. |
| Embedded Go SSH client | rejected | esheep would own key loading, agent, known_hosts, and ssh config parsing. |
| Mirror transcripts locally (Syncthing, rsync) | rejected | Duplicates gigabytes, reports stale data, needs a mirror per harness per machine. |
| Long-running daemon with HTTP API | rejected | Auth, TLS, and a process to keep alive for a personal fleet. |

### Why results-only matters

Search decodes every transcript that passes the metadata filters. Under a file-pull model those bytes cross the wire on every query. Measured on one machine as a proxy for a remote:

| Root | Size | Files |
|---|---|---|
| Claude Code | 1.2 GB | 9,617 |
| Pi | 1.6 GB | 17,725 |
| Codex active + archived | 409 MB | 382 |
| Cowork | 245 MB | 1,494 |
| Claude Code files touched in the last 7 days | 72 MB | |

With esheep on the remote, the wire carries one JSON document of matches. An unfiltered search is as cheap as a bounded one, so no time filter is required for remote queries.

## Command-line contract

Both `sessions list` and `sessions search` gain two flags through the shared filter flag set. Every existing flag keeps its meaning and is forwarded to each remote.

| Flag | Meaning |
|---|---|
| `--remote NAME[,NAME...]` | Add configured machines to the local scan. Repeatable or comma-separated. `all` selects every configured machine and cannot combine with names. |
| `--no-local` | Drop the local machine. Requires `--remote`, otherwise nothing would be searched and it is a usage error. |

```sh
esheep sessions search 'timeout'                                # local only, unchanged
esheep sessions search 'timeout' --remote nas                   # local and nas
esheep sessions search 'timeout' --remote nas --no-local        # nas only
esheep sessions search 'timeout' --remote nas,studio --since 7d # local and both remotes, last week
esheep sessions list --remote all --project esheep              # every machine, one project
esheep sessions search 'timeout' --remote laptop9               # usage error: unknown machine, exit 2
esheep sessions search 'timeout' --no-local                     # usage error: nothing to search, exit 2
```

Unknown names exit 2 like unknown harness names. Duplicate names collapse silently. `--remote all` with zero configured machines returns local results with a nonfatal diagnostic naming `[[machines]]`.

## Configuration

```toml
# esheep.toml (TOML only, no env or flag form)

[[machines]]
name = "nas"            # hostname; ssh destination defaults to it

[[machines]]
name = "studio"
host = "javier@studio.tail1234.ts.net"
command = "/opt/homebrew/bin/esheep"   # default "esheep"
timeout = "2m"                          # default 60s

[[machines]]
name = "mba"
command = "~/bin/esheep"
```

- An entry has exactly four keys: `name`, `host`, `command`, `timeout`. Transcript locations are the remote's own business.
- Identity is the hostname. The entry whose first label matches the first label of `os.Hostname()`, case-insensitively, is this machine. `--remote all` skips it. Passing your own hostname to `--remote` is a usage error.
- Local results are labeled with the local hostname whether or not it is configured. The literal `local` never appears in output.
- `command` is inserted verbatim into the ssh command line, so it can point at a Homebrew path or be quoted as the remote shell requires. Non-interactive login shells often lack Homebrew on PATH, which is why the key exists.
- Validation at load: names unique and non-empty, `all` reserved, `timeout` a Go duration.
- `esheep config` prints every machine entry with its effective values.

## What runs on the remote

```
ssh -o BatchMode=yes -o ConnectTimeout=10 javier@studio.tail1234.ts.net /opt/homebrew/bin/esheep sessions query
```

Request on stdin:

```json
{
  "mode": "search",
  "filter": {
    "archive_state": "all",
    "harnesses": [],
    "ids": [],
    "project": "",
    "since": "2026-09-06T21:47:06-04:00",
    "until": null,
    "subagents": false
  },
  "query": { "pattern": "timeout", "role": "", "tool": "", "errors": false, "raw": false }
}
```

Stdout is exactly the document `esheep sessions search --json` would print on that machine.

- Fixed command line. No user data ever touches the remote login shell, so bash, zsh, sh, and fish behave identically. Only the configured `command` string appears.
- Batch mode. An unknown host key or a passphrase without an agent fails with a message telling the user to ssh to that host manually once.
- Own config. The endpoint discovers config exactly as the remote would on its own. `--config` and `ESHEEP_*` variables are never forwarded.
- No recursion. The endpoint never consults its own `[[machines]]`.

## The endpoint: `esheep sessions query`

- Input is one JSON request on stdin. `mode` is `list` or `search`. `filter` carries every shared filter flag. `query` carries the search flags and is ignored for `list`.
- Times are instants. `since` and `until` arrive as RFC 3339, already resolved on the caller's clock, so `--since 7d` means the same window on every machine and a skewed remote clock cannot move it.
- Output is the exact document `list --json` or `search --json` writes, with its existing `complete`, `diagnostics`, and `sessions`. The endpoint does not know it is remote.
- Validation matches the flags: a search needs a pattern, `--tool`, or `--errors`; `raw` cannot combine with structural criteria; empty IDs are rejected. Invalid requests exit 2 with the reason on stderr.
- Exit status follows the underlying command: 0 complete, 1 incomplete or application failure, 2 usage.
- Documented in help as the machine-to-machine entry point, with the request schema, since help is the canonical contract. Humans can drive it with a file on stdin.

## Local pipeline

1. Validate flags. Unknown machine, `all` with names, `--no-local` without `--remote`, and naming your own hostname are usage errors before anything runs.
2. Resolve the request once. `--since` and `--until` become instants on the local clock. The same request goes to every machine.
3. Fan out. The local scan and every selected machine run concurrently, each machine under its own timeout.
4. Parse each reply. Stdout must be one JSON document of the expected top-level shape starting at the first byte. Unknown fields are ignored and missing fields zero-filled. The remote exit status is ignored in favor of the document's `complete`.
5. Stamp and merge. Every session and diagnostic gets `machine` set to its hostname. Sessions interleave most-recent-first across machines with hostname as the tiebreak. Diagnostics concatenate. `complete` is the conjunction of every machine's result.
6. Print with the same writers as today, with the machine column and prefix added when `--remote` is present.

## Output

JSON:

- Every session and every diagnostic carries `"machine": "<hostname>"`, always present, including local-only runs.
- `path` is unchanged: the absolute path on the machine that owns the file.
- `complete`, `diagnostics`, `sessions`, and hit shapes are otherwise identical to today.

```json
{
  "complete": false,
  "diagnostics": [
    {"code": "machine-timeout", "machine": "laptop", "message": "no reply within 60s"}
  ],
  "sessions": [
    {"machine": "nas", "harness": "claude", "id": "…", "path": "/home/j/.claude/projects/…",
     "hits": [{"line": 42, "role": "user", "excerpt": "…"}]}
  ]
}
```

Text:

- Local-only output does not change at all.
- When `--remote` is present, the list table gains a MACHINE column and each search header includes the hostname.
- Diagnostics on stderr are prefixed with the hostname, as `host: code: message` beside the existing `path [harness]: code: message` form.

```
MACHINE  HARNESS  ID     STARTED           ARCHIVE STATE  …
nas      claude   a1b2…  2026-09-12 09:14  active         …
mba      pi       c3d4…  2026-09-11 22:40  active         …

laptop: machine-timeout: no reply within 60s
```

## Failures, timeouts, and exit codes

A machine that cannot answer makes the report incomplete, the command exits 1, and every result that did arrive is still printed. This is the existing rule for an unreadable local root, applied to a machine.

| Code | When | Message carries |
|---|---|---|
| `machine-unreachable` | ssh exits 255: DNS, refused, batch-mode auth failure, unknown host key | last lines of ssh stderr |
| `machine-timeout` | the whole call exceeded the entry's `timeout` | the timeout value |
| `machine-command` | any other nonzero exit with no document, such as esheep missing from PATH or a broken remote config | last lines of remote stderr |
| `machine-reply` | stdout is not one document of the expected shape, including a shell rc file that prints to stdout | decoder error and the leading bytes |

- Timeout: one per entry, default 60s, covering connect through last byte. ssh `ConnectTimeout` is the smaller of 10s and the entry's timeout.
- No version check. Different builds interoperate on a best-effort basis. After a breaking change to the request or document shape, a stale remote may return wrong or empty results until it is upgraded. Help says so.
- Exit codes are unchanged: 0 success, 1 incomplete or application failure, 2 usage. JSON mode reports failure inside the document and stays silent on stderr, as today.

## Edge cases

- Sleeping laptop in `--remote all`: incomplete, exit 1, other machines' results printed. Name the awake machines to avoid it.
- Shared `esheep.toml` across machines: hostname identity skips self under `all`. No duplicate results.
- Own hostname passed explicitly: usage error, exit 2.
- Same session ID on two machines: two results, distinguished by `machine`, never merged.
- esheep not on the remote's non-interactive PATH: `machine-command` with the shell's "command not found" in the message. Fix with `command`.
- Remote login shell is fish: irrelevant, the command line carries no user data.
- Remote rc file prints to stdout: `machine-reply` naming the leading bytes, so the broken setup is visible instead of skipped over.
- MOTD or banners on stderr: captured, never mixed into stdout, surfaced only inside a failure diagnostic.
- Remote clock skew: cutoffs are instants resolved locally, so the window is identical everywhere. Only the remote's file timestamps matter, as they do today.
- Stale build on one machine: best-effort decode. Unknown fields ignored, missing fields zero-filled. Documented as the cost of skipping a version check.
- Remote config missing or invalid: the remote esheep exits 1 with its usual error; the local side reports `machine-command` carrying that text.
- Remote harness path unconfigured, such as Cowork on Linux: the remote's own nonfatal diagnostic arrives in the merged diagnostics, stamped with the hostname.
- Remote scan incomplete: its `complete: false` makes the merged report incomplete. Its diagnostics explain why.
- Subagents, raw search, IDs, projects, archive state: all forwarded in the request and applied by the remote with the same code that runs locally.
- Huge result set: memory is the size of the reply document, not the transcripts.
- Endpoint run by hand with no stdin: usage error naming the request schema in help.

## Documentation and contract changes

- The no-network promise is rewritten, not caveated. Root help and README currently say esheep never accesses the network. They will say esheep contacts a machine only when `--remote` names it, and only to run esheep there over ssh. Clean-break rule: no old wording survives.
- Sessions help documents `--remote`, `--no-local`, batch-mode behavior, the four diagnostic codes, the best-effort version stance, and the `machine` field in the JSON contract.
- `esheep sessions query --help` documents the request schema, output, validation, and exit status.
- `esheep config --help` documents `[[machines]]`, its four keys, defaults, validation, and hostname identity.
- README gets one paragraph pointing at the feature and the help topics.
- `esheep help exit-codes` is unchanged.
- `esheep doctor` stays offline. Verify a machine with `esheep sessions list --remote <name> --since 1d`.

## Implementation plan

1. Request type. In `internal/session`, a `Request` struct that round-trips `Filter` and `SearchQuery` through JSON, with times as RFC 3339 and the pattern as source text compiled on receipt.
2. Endpoint command. `cmd/sessions_query.go`: read stdin, decode and validate the request, load config the normal way, run `List` or `Search`, write the existing JSON document. Reuse the existing operation seams so tests inject fakes.
3. Config. Add `[[machines]]` to `internal/config` with validation, defaults, hostname identity, and `esheep config` output.
4. Remote runner. New `internal/remote`: build the ssh argument list, run it with the request on stdin under the entry's timeout, parse the reply, map failures to diagnostic codes, stamp the hostname.
5. Fan-out and merge. Run local plus machines concurrently, merge sessions and diagnostics, conjoin `complete`. Lives beside the runner so `internal/session` stays ignorant of machines.
6. Command wiring. Register `--remote` and `--no-local` in the shared filter flags, enforce the usage errors, extend text writers in `internal/ui` with the machine column and stderr prefix.
7. Docs, then `make check`, then a real run against the second machine before merge.

## Testing strategy

- Unit, request round-trip: every flag combination encodes to a request and decodes to an equal `Filter` and `SearchQuery`, including instants, IDs with commas and quotes, and regex source.
- Unit, endpoint: valid requests produce byte-identical documents to the flag-driven commands; invalid requests exit 2 with the same messages the flags give.
- Unit, runner: a fake `ssh` script first on PATH records its arguments and stdin, then echoes canned JSON, exits 255, sleeps past the timeout, or prints junk. Assert each diagnostic code and message.
- Unit, merge: ordering across machines, tiebreaks, `complete` conjunction, hostname stamping, self-skip under `all`, every usage error.
- End to end: the e2e suite's fake `ssh` runs the received command line locally against the built binary and a fixture home, so the whole path from flag to merged output is exercised with no network, including a real `sessions query` invocation.
- Manual: one real run against the second machine before merge, including a fish login shell and a Homebrew-only PATH.

## Decision ledger

### Interview

| | Question | Answer |
|---|---|---|
| Q1 | How does esheep reach remote transcripts? | esheep installed on each remote, invoked over the OpenSSH binary, results only. Revisited after a first pass at a file pull. |
| Q2 | How are machines selected? | `--remote` adds to local, `--no-local` removes it, `all` selects every entry. |
| Q3 | Does esheep know which machine it is? | Yes, by hostname. Entry names are hostnames, self is skipped under `all`, local results are labeled with the local hostname. |
| Q4 | What happens when a machine fails? | Report incomplete, exit 1, results still printed. Per-entry timeout with a default. |
| Q5 | Merged document shape? | One merged list; every session and diagnostic gains `machine`. Nothing else changes. |
| Q6 | How is the query handed over? | A stdin JSON endpoint, `esheep sessions query`, with a fixed ssh command line. |
| Q7 | Different builds? | No version check. Best-effort decode. |

### Source-resolved

| Source | Question | Answer |
|---|---|---|
| AGENTS.md | Never prompt | ssh runs with `BatchMode=yes` |
| AGENTS.md | Payloads stdout, diagnostics stderr | Remote stderr is captured and only surfaces in diagnostics |
| AGENTS.md | Clean-break mode | No-network statement is rewritten, not caveated |
| AGENTS.md | Help is canonical | Every new contract lands in command help first |
| cmd/sessions.go | Which commands get the flags | Both, via the shared filter flag set |
| internal/config/config.go | Where machines are configured | TOML only, like sources |
| internal/session/session.go | Derived state | None; replies live in memory for one run |
| cmd/exitcodes.go | Unknown machine name | Usage error, exit 2, like an unknown harness |

### Approved judgment calls

1. Machine entry has exactly `name`, `host` (default name), `command` (default `esheep`, inserted verbatim), `timeout` (default 60s).
2. `esheep sessions query` reads one request on stdin, forwards every filter and search flag with times as instants, resolves the remote's own config, never fans out, and is documented in help.
3. Reply must be one document starting at the first byte; remote exit status is ignored in favor of `complete`; codes are `machine-unreachable`, `machine-timeout`, `machine-command`, `machine-reply`.
4. Cutoff instants come from the local clock and are forwarded, so remote clock skew cannot move the window.
5. Remote stderr diagnostics print as `host: code: message`.
6. Hostname identity compares first labels case-insensitively; names unique, `all` reserved; naming yourself is a usage error.
7. One timeout per entry covering the whole call; ssh `ConnectTimeout` is min(10s, timeout); machines and the local scan run concurrently.
8. Text adds a MACHINE column and header hostname only when `--remote` is present; interleaved ordering with hostname tiebreak.
9. No doctor check; verification is a list against the machine.

## Open questions

- Default timeout. 60s is a guess. A wide unfiltered search on a slow machine may need more; the per-entry key covers it.
- Version drift symptoms. With no check, a stale remote after a breaking change may return an empty or odd document rather than an error. If that bites, a protocol number is the fallback and would be a small addition.
- Request schema stability. The request mirrors the flags one-to-one, so any flag change is a request change. That is intended under clean-break mode but worth remembering when adding filters.

## Next step

The first landable piece is the request type plus the `sessions query` endpoint, which is useful on its own and testable without any ssh.
