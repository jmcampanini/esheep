# Milestones

## 1. Skill management (current)

Manage skills from multiple git repositories and sync them into every configured harness. Skill repos hold [Agent Skills spec](https://agentskills.io/specification)–conformant skills with optional per-harness frontmatter blocks; `esheep sync` renders each skill per target — Claude Code, Pi, Codex (including the generated `agents/openai.yaml` policy file), and the shared `~/.agents/skills` directory — and `esheep lint` catches anything that does not work across all harnesses.

Designed. Implementation plan: `PLAN.md` (local, untracked).

## 2. Running-session management (unplanned)

Quick-switch and an overview of coding agents currently running across tmux sessions on the same machine. Open question: whether agents must self-register via a hook, or whether esheep can inspect existing tmux panes and processes to detect an agent and its state.

## 3. Session history (unplanned)

"ripgrep for sessions": search previous sessions across all harnesses — find all sessions, or the session, matching a query (for example, "search for X in my codex sessions"). Built on parsing each harness's differing session file format and canonicalizing them into one standardized session-log format so sessions can be reviewed and understood as one language rather than several.
