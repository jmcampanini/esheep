package cmd

// Shared help fragments compose command long descriptions so repeated
// contract text cannot drift between commands. Fragments carry no leading or
// trailing newline; compose them with explicit separators.

const streamContractHelp = `Payload goes to stdout; human-readable diagnostics and final error messages
go to stderr. Human tables use color only on terminals and honor NO_COLOR;
redirected output contains no terminal escapes.`

const jsonContractHelp = `--json emits one complete JSON document, including structured diagnostics,
to stdout and does not duplicate an unsuccessful report as a stderr error;
the exit status still reports failure.`

const configResolutionHelp = `Settings load in this order, with later sources taking precedence: built-in
target and session defaults (CODEX_HOME supplies the Codex session home default
when set); $XDG_CONFIG_HOME/esheep/esheep.toml, or
$HOME/.config/esheep/esheep.toml; ESHEEP_PROFILES, ESHEEP_<TARGET>_ENABLED,
ESHEEP_<TARGET>_SKILLS_PATH, ESHEEP_<TARGET>_AGENTS_MD_PATH, and
ESHEEP_CLAUDE_SESSIONS_PATH, ESHEEP_CLAUDE_COWORK_SESSIONS_PATH,
ESHEEP_PI_SESSIONS_PATH, and ESHEEP_CODEX_HOME
variables; then the profile, target, and
session flags. --config PATH replaces automatic discovery and requires a
loadable file. Source directories and env_profiles are configured only in
the TOML file; every environment variable named by env_profiles appends its
comma-separated profiles to the effective list.`

const sessionRemoteHelp = `--remote NAME[,NAME...] adds configured machines to the scan. Repeat the flag
or separate names with commas; 'all' selects every configured machine and
cannot combine with names. --no-local drops this machine and requires
--remote. Every other flag keeps its meaning and is forwarded to each
machine. Without --remote nothing leaves this machine.

Machines are [[machines]] entries in esheep.toml, identified by hostname;
'esheep config --help' describes them. The entry whose first hostname label
matches this machine's is skipped by 'all', and naming it is a usage error,
as is naming an unknown machine. 'all' with no other machine configured
reports a no-machines diagnostic and scans locally.

Each selected machine runs 'ssh -o BatchMode=yes -o ConnectTimeout=<n>
<host> <command> sessions query' with the request on stdin, concurrently
with the local scan and under the entry's timeout. The remote esheep reads
its own configuration and transcripts and returns only results, so a
remote host key or passphrase prompt cannot appear: connect to the host
with ssh once by hand instead. Local and remote results interleave most
recent first, and every session and diagnostic names its machine. Text
output adds a MACHINE column or header field and prefixes stderr
diagnostics with the machine name only when --remote is present.

A machine that cannot answer makes the report incomplete and the command
exit 1 while every result that did arrive is printed. Its diagnostic is
machine-unreachable (ssh failed: name resolution, connection, batch-mode
authentication, or an unknown host key), machine-timeout (no complete
reply within the entry's timeout), machine-command (the remote command
exited without a document, such as esheep missing from the login shell's
PATH or an invalid remote configuration), or machine-reply (stdout was not
one JSON document, such as a shell startup file printing to stdout). The
message carries the last lines of the remote's stderr or the decoder error.

Builds are not version-checked. Replies from a different esheep build
decode best-effort: unknown fields are ignored and missing fields are
zero-filled, so a stale remote can return wrong or empty results after a
change to the request or document shape until it is upgraded.`
