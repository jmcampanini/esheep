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
target defaults; $XDG_CONFIG_HOME/esheep/esheep.toml, or
$HOME/.config/esheep/esheep.toml; ESHEEP_PROFILES, ESHEEP_<TARGET>_ENABLED,
ESHEEP_<TARGET>_SKILLS_PATH, and ESHEEP_<TARGET>_AGENTS_MD_PATH variables;
then the profile and target flags. --config PATH replaces automatic
discovery and requires a loadable file. Source directories and
env_profiles are configured only in the TOML file; every environment
variable named by env_profiles appends its comma-separated profiles to the
effective list.`
