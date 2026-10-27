package cli

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/remote"
	"github.com/jmcampanini/esheep/internal/session"
	"github.com/spf13/cobra"
)

const sessionHarnessHelp = `claude-cowork reads local Cowork conversations from
[sessions.claude-cowork].path. Its precedence is --claude-cowork-sessions-path,
ESHEEP_CLAUDE_COWORK_SESSIONS_PATH, TOML, then the platform default. On macOS,
the default is ~/Library/Application Support/Claude/local-agent-mode-sessions.
Linux requires an explicit path; an empty path disables Cowork discovery.
Only <scope>/<scope>/local_<id>/audit.jsonl files are discovered. Scope names
are opaque. Supporting files and regular Claude chats are not session sources.

The codex harness includes Codex CLI and desktop sessions. chatgpt-work
identifies local ChatGPT Work tasks whose recorded originator is exactly
codex_work_desktop. Missing or unrecognized originators are classified as codex.
Subagents use their own recorded originator.

Both harnesses share [sessions.codex].home. Its precedence is --codex-home,
ESHEEP_CODEX_HOME, TOML, CODEX_HOME, then ~/.codex. The sessions and
archived_sessions directories beneath that home are read in place; no other
home is scanned. Shared storage is discovered once; root diagnostics use
codex. Omit --harness to include all harnesses, or use
--harness codex,chatgpt-work to select both.

Work and Cowork transcripts qualify when they contain a supported user message,
assistant message, tool call, or tool result. Title-only and
injected-context-only records are excluded, including from --raw searches.
Partial or unknown local history qualifies; results do not imply complete
remote history. A transcript's history_base may reference earlier history
elsewhere. That history is not expanded or searched through the reference;
hits address only saved lines in each discovered file. Cowork uses its
session folder's relative path, including both scopes, as its ID.
Companion metadata supplies title, creation time,
selected host folders, and the archive flag. Missing or invalid companions
do not exclude readable messages. Invalid or mismatched companions produce
diagnostics; a mismatched sessionId discards that companion's metadata.
Cowork event times prefer timestamp, then _audit_timestamp. A missing creation
time falls back to the first recorded event time.

Every session has a projects array. Claude Code and Pi supply their recorded
working directory. Cowork supplies userSelectedFolders, excluding its execution
working directory. Codex and Work supply the distinct workspace_roots across
saved turn_context records, falling back to the session header's cwd when no
workspace roots are recorded. --project matches any path, case-insensitively.
Unknown folders produce an empty array and do not match --project.

Active and archived transcripts are included by default. --archive-state
all|active|archived selects archive state independently of --subagents.
Cowork uses its companion's isArchived flag; absent or invalid evidence means
not archived. Claude and Pi are treated as active. Both Codex directories
are discovered before filtering so overlapping files have one archive state.
Repeated physical files, including hard links, appear once; archive placement
and its path win when a file occurs in both locations. Distinct files with
the same session ID remain separate.

Primary activity includes requests to subagents and their returned results.
--subagents additionally includes child activity, including Cowork records
marked with parent_tool_use_id inside the primary audit. An audit remains one
session result. --raw searches all original lines in included transcripts,
including embedded child activity, without decoding event roles.

Selecting a harness with no configured path reports a nonfatal diagnostic
naming its setting. Disabled harnesses are skipped quietly when --harness is
omitted. Missing locations are skipped with diagnostics. Unreadable inputs and
detected file moves make the scan incomplete; rerun after moves finish.
Unreadable Cowork companions make the scan incomplete while preserving
readable hits. Scans are not atomic snapshots and do not retry. "complete"
describes the filesystem scan, not conversation or remote-history
completeness.`

const sessionIDHelp = `--id selects exact, case-sensitive session IDs. Repeat the flag or use
comma-separated IDs to select any of them; all other filters still apply,
including --subagents. Empty IDs are usage errors. Cowork accepts either its
full scoped ID or its complete local_<id> component; the latter selects every
matching scope. Output retains full scoped IDs. Multiple files sharing an ID
remain separate results; no matches is a successful empty result when the scan
completes.

ID values cannot contain line breaks. Values use CSV quoting: to select the
ID a"b,c, pass --id '"a""b,c"'.

ID filtering rejects nonmatches before further metadata reads where possible.
Claude and Cowork use path-derived IDs. Codex and Work read the first-line
header, whose ID takes precedence over the filename fallback. Pi retains its
bounded metadata read. Matching sessions keep the same metadata and eligibility
rules as an unfiltered scan. Errors in skipped content or companion metadata
are not reported; discovery failures and failures reading required IDs or
matching transcripts still affect completeness.`

const sessionPeriodHelp = `--since and --until select activity by event timestamps recorded in each
transcript. File modification time and session start do not determine period
membership or skip candidate transcripts. An event qualifies when
since <= timestamp < until; omitted bounds are unrestricted.

Dates are inclusive local calendar days on the calling machine. --since starts
at local midnight on that date; --until ends at local midnight on the next date.
Day counts (7d) and durations (36h) subtract elapsed time from one caller clock
reading and resolve to exact instants at either bound. Remotes receive those
resolved instants and never reinterpret dates. A since at or after until is a
usage error naming both resolved instants.

Under a period, undated events never qualify. Search reports undated-events
for events that match its other criteria but lack usable timestamps. List
reports it when undated events exist and none qualify before EOF. These
diagnostics do not change complete. List stops at its first qualifying event
and reports malformed-lines only for the portion scanned. --raw cannot combine
with --since or --until.

List and search JSON always include period with resolved since and until
instants, each null when open.`

const sessionRemoteHelp = `--remote NAME[,NAME...] adds configured machines to the scan. Repeat the flag
or separate names with commas; 'all' selects every configured machine and
cannot combine with names. --no-local drops this machine and requires
--remote. Every other flag keeps its meaning and is forwarded to each
machine. Without --remote nothing leaves this machine.

Machines are [[machines]] entries in config.toml, identified by hostname;
'esheep config --help' describes them. The entry whose first hostname label
matches this machine's is skipped by 'all', and naming it is a usage error,
as is naming an unknown machine. 'all' with no other machine configured
reports a no-machines diagnostic and scans locally.

Each selected machine runs 'ssh -T -o BatchMode=yes -o ConnectTimeout=<n>
<host> <command> sessions query' with the request on stdin, concurrently
with the local scan and under the entry's timeout. The remote esheep reads
its own configuration and transcripts and returns only results, so a
remote host key or passphrase prompt cannot appear: connect to the host
with ssh once by hand instead. Local and remote results interleave most
recent first, and every session names its machine. Text output adds a
MACHINE column or header field. In text and JSON modes, stderr diagnostics
carry the machine name only when --remote is present.

A machine that cannot answer makes the report incomplete and the command
exit 1 while every result that did arrive is printed. Its diagnostic is
machine-unreachable (ssh failed: name resolution, connection, batch-mode
authentication, or an unknown host key), machine-timeout (no complete
reply within the entry's timeout), machine-command (the remote command
exited without a document, such as esheep missing from the login shell's
PATH or an invalid remote configuration), or machine-reply (stdout was not
one JSON document, such as a shell startup file printing to stdout). The
message carries the last lines of the remote's stderr or the decoder error.

Builds are not version-checked. A remote rejects a request field it does
not know and answers with a machine-command diagnostic, while replies
decode best-effort: unknown fields are ignored and missing fields are
zero-filled. A machine running a different esheep build can therefore
refuse the request, ignore a filter, or return wrong or empty results
after a change to the request or document shape until it is upgraded.`

func newSessionsCommand(load configLoader, operations commandOperations) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Find historical harness sessions",
		Long: `Find historical session transcripts recorded by Claude Code, Pi,
Codex, local ChatGPT Work tasks, and local Claude Cowork conversations,
reading the harness-owned files in place.

Transcripts are read-only inputs: esheep never creates, updates, or deletes
anything under a session root and keeps no copies or indexes. Every result
points at the canonical transcript file so the original can be read directly.

Session roots default to ~/.claude/projects, ~/.pi/agent/sessions,
~/.codex/sessions, and ~/.codex/archived_sessions. Configure the Claude and Pi
paths and the Codex home under [sessions] in TOML; Codex transcript paths are
derived from its home. Cowork's platform defaults and configuration are
described below. A missing root skips that location with a diagnostic.

A bare 'esheep sessions' prints this help on stdout. Every command in
this group writes its report to stdout and diagnostics to stderr, offers
the report as one JSON document, and never prompts. Commands that accept
--remote run esheep on configured machines over ssh; without it nothing
leaves this machine.

` + sessionHarnessHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		newSessionsListCommand(load, operations),
		newSessionsQueryCommand(load, operations),
		newSessionsSearchCommand(load, operations),
	)
	return command
}

// sessionFilterFlags carries the raw flag values shared by list and search.
type sessionFilterFlags struct {
	archiveState string
	harnesses    []string
	ids          []string
	noLocal      bool
	project      string
	remote       []string
	since        string
	subagents    bool
	until        string
}

func registerSessionFilterFlags(command *cobra.Command, flags *sessionFilterFlags) {
	command.Flags().StringVar(&flags.archiveState, "archive-state", "all", "limit to archive state (all, active, archived)")
	command.Flags().StringSliceVar(&flags.harnesses, "harness", nil, "limit to harnesses (chatgpt-work, claude, claude-cowork, codex, pi); repeatable or comma-separated")
	command.Flags().StringArrayVar(&flags.ids, "id", nil, "limit to exact session IDs; repeatable or comma-separated")
	command.Flags().BoolVar(&flags.noLocal, "no-local", false, "skip this machine; requires --remote")
	command.Flags().StringVar(&flags.project, "project", "", "limit to sessions with any project path containing this text")
	command.Flags().StringSliceVar(&flags.remote, "remote", nil, "also query configured machines by name, or all; repeatable or comma-separated")
	command.Flags().StringVar(&flags.since, "since", "", "keep activity recorded at or after a day count (7d), duration (36h), or local date (2026-01-02)")
	command.Flags().BoolVar(&flags.subagents, "subagents", false, "include subagent transcripts and embedded child activity")
	command.Flags().StringVar(&flags.until, "until", "", "keep activity recorded before a day count, duration, or the end of a local date")
}

// requestFilter converts the flag values into the request form, resolving
// relative times against now so every machine applies the same instants.
func (f sessionFilterFlags) requestFilter(now time.Time) (session.RequestFilter, error) {
	filter := session.RequestFilter{ArchiveState: f.archiveState, Harnesses: append([]string{}, f.harnesses...), IDs: []string{}, Project: f.project, Subagents: f.subagents}
	for _, value := range f.ids {
		if value == "" {
			return session.RequestFilter{}, errors.New("--id must not be empty")
		}
		if strings.ContainsAny(value, "\r\n") {
			return session.RequestFilter{}, errors.New("--id must not contain line breaks")
		}
		ids, err := csv.NewReader(strings.NewReader(value)).Read()
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--id: parse comma-separated IDs: %w", err)
		}
		filter.IDs = append(filter.IDs, ids...)
	}
	if f.since != "" {
		since, err := session.ParseSince(f.since, now)
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--since: %w", err)
		}
		filter.Since = &since
	}
	if f.until != "" {
		until, err := session.ParseUntil(f.until, now)
		if err != nil {
			return session.RequestFilter{}, fmt.Errorf("--until: %w", err)
		}
		filter.Until = &until
	}
	return filter, nil
}

func sessionRoots(loaded config.LoadResult) session.Roots {
	return session.Roots{
		Claude:                loaded.ResolvedSessions.Claude,
		ClaudeCowork:          loaded.ResolvedSessions.ClaudeCowork,
		CodexArchivedSessions: loaded.ResolvedSessions.Codex.ArchivedSessions,
		CodexSessions:         loaded.ResolvedSessions.Codex.Sessions,
		Pi:                    loaded.ResolvedSessions.Pi,
	}
}

// sessionScope is the set of machines one session command queries, the ssh
// client that reaches them, and any selection diagnostic to report.
type sessionScope struct {
	diagnostics []session.Diagnostic
	selection   remote.Selection
	ssh         string
}

// resolveSessionScope selects machines against the loaded configuration.
// Selection failures are usage errors; a missing ssh client is an
// application error and is only checked when a machine is selected.
func resolveSessionScope(operations commandOperations, loaded config.LoadResult, names remote.Names) (sessionScope, error) {
	hostname, err := operations.localHostname()
	if err != nil {
		return sessionScope{}, apperr.Wrap(fmt.Errorf("determine hostname: %w", err))
	}
	selection, diagnostics, err := names.Select(loaded.ResolvedMachines, hostname)
	if err != nil {
		return sessionScope{}, err
	}

	scope := sessionScope{diagnostics: diagnostics, selection: selection}
	if len(selection.Machines) != 0 {
		scope.ssh, err = exec.LookPath("ssh")
		if err != nil {
			return sessionScope{}, apperr.Wrap(fmt.Errorf("locate ssh client: %w", err))
		}
	}
	return scope, nil
}

// localHostname names this machine, using the operating system when no
// hostname operation is injected.
func (o commandOperations) localHostname() (string, error) {
	if o.hostname == nil {
		return os.Hostname()
	}
	return o.hostname()
}
