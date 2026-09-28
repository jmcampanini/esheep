package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jmcampanini/esheep/internal/memory"
	"github.com/spf13/cobra"
)

func newMemoryCommand(load configLoader, record func(context.Context, memory.Request) (memory.Result, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "memory",
		Short: "Record memories from harness sessions",
		Long: `Record memories that coding agents want to keep beyond one session.

'memory record' appends one entry to a file named by the calling harness
session inside a directory named by the project the session works in. The
root defaults to $XDG_DATA_HOME/esheep/memory, or
~/.local/share/esheep/memory, and is configured by [memory].path in TOML,
ESHEEP_MEMORY_PATH, or --memory-path. esheep only ever appends under that
root; reading and reviewing recorded memories is not part of this command.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(newMemoryRecordCommand(load, record))
	return command
}

func newMemoryRecordCommand(load configLoader, record func(context.Context, memory.Request) (memory.Result, error)) *cobra.Command {
	var session, why string
	var sources []string
	command := &cobra.Command{
		Use:   "record TEXT",
		Short: "Append one memory for the current session",
		Long: `Append one memory to <root>/<project>/memory/<session>.jsonl and print that
file's absolute path on stdout.

TEXT is the memory. Pass '-' to read it from stdin until EOF instead; on a
terminal the command then waits for EOF and never prompts. Blank text is a
usage error. --why records the reason the memory holds and --source records
pointers backing it, such as file paths, URLs, commits, or session IDs;
repeat --source for several.

Each entry is one JSON object on its own line with keys in this order:
time (RFC 3339 with the local offset), cwd (the working directory), text,
then why and sources only when given. Text is stored as given. The append
is a single write, so entries from concurrent invocations never interleave.

The session comes from --session, else from the first of CODEX_THREAD_ID,
PI_SESSION_ID, and CLAUDE_CODE_SESSION_ID that is set. That order picks the
innermost harness when one harness runs inside another. Codex, Pi, and
Claude Code set those variables for the commands they run, and each value
is the session ID that 'esheep sessions' reports. When none is set the
command exits 1 naming the variables; --session lets a person record by
hand. A session ID must be non-empty and free of path separators, '..',
whitespace, and control characters, because it becomes the file name.

The project is derived from the working directory. Inside a git repository
it is the origin remote as host/org/repo, with the scheme, user info,
port, and .git suffix removed and the host lowercased, so every clone and
worktree of one repository shares one project. A repository without an
origin, or whose origin is a local path, uses the main repository root,
taken as the parent of the shared git directory; a submodule or a
repository with a separate git directory therefore lands under that
parent instead. A missing git binary uses the worktree root and reports a
diagnostic on stderr. Outside git the project is the working directory. The project
directory name is that identity with every '/' replaced by '-', so
github.com/acme/widgets becomes github.com-acme-widgets and
/Users/me/scratch becomes -Users-me-scratch.

The command creates missing directories and files under the memory root
and touches nothing else. It never contacts another machine.

` + streamContractHelp,
		Example: `  esheep memory record 'Prefer merge over rebase when syncing with main' \
    --why 'the user wants one merge commit' --source README.md
  esheep memory record - <<'EOF'
  Multi-line memory text.
  EOF`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if session != "" {
				if err := memory.ValidateSession(session); err != nil {
					return fmt.Errorf("--session: %w", err)
				}
			}
			for _, source := range sources {
				if strings.TrimSpace(source) == "" {
					return errors.New("--source must not be blank")
				}
			}
			text, err := memoryText(command.InOrStdin(), args[0])
			if err != nil {
				return err
			}

			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return appError(fmt.Errorf("determine working directory: %w", err))
			}
			result, err := record(command.Context(), memory.Request{
				Cwd:     cwd,
				Env:     processEnvironment(),
				Root:    loaded.ResolvedMemory,
				Session: session,
				Sources: sources,
				Text:    text,
				Time:    time.Now(),
				Why:     why,
			})
			if err != nil {
				return appError(err)
			}

			for _, diagnostic := range result.Diagnostics {
				_, _ = fmt.Fprintln(command.ErrOrStderr(), diagnostic)
			}
			_, _ = fmt.Fprintln(command.OutOrStdout(), result.Path)
			return nil
		},
	}
	command.Flags().StringVar(&session, "session", "", "session ID to record under instead of detecting one")
	command.Flags().StringArrayVar(&sources, "source", nil, "pointer backing the memory; repeatable")
	command.Flags().StringVar(&why, "why", "", "reason the memory holds")
	return command
}

// memoryText returns the operand, or stdin read to EOF when the operand is
// '-'. Blank text is a usage error either way.
func memoryText(stdin io.Reader, operand string) (string, error) {
	text := operand
	if operand == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", appError(fmt.Errorf("read text from stdin: %w", err))
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("TEXT must not be blank")
	}
	return text, nil
}

// processEnvironment converts the process environment into a lookup map.
func processEnvironment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	return env
}
