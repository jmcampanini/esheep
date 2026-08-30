package cmd

import (
	"github.com/spf13/cobra"
)

func newSkillFormatTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "skill-format",
		Short: "Skill directory layout and SKILL.md frontmatter",
		Long: `Each source is a read-only collection of top-level skill directories: an
immediate child directory containing SKILL.md is a skill. Dot-directories,
node_modules, and symlinked children are skipped. Supporting files are
validated and rendered as non-executable data; supporting paths must be
unique under case-insensitive Unicode-normalized comparison, and absolute,
escaping, cyclic, and directory symlinks are rejected. The root .esheep.toml
name is reserved for ownership metadata.

SKILL.md is YAML frontmatter followed by a Markdown body preserved
byte-for-byte. Frontmatter fields fall into two categories: fields esheep
interprets and validates, and fields it passes through verbatim.

Interpreted fields:

  name                      Required. 1-64 characters, lowercase
                            alphanumeric words separated by single hyphens,
                            equal to the skill directory name.
  description               Required and nonempty. At most 1024 characters.
  license                   Optional string.
  compatibility             Optional string. At most 500 characters.
  metadata                  Optional string-to-string map.
  disable-model-invocation  Optional boolean. When true, rendered output
                            tells each target not to invoke the skill
                            automatically.

Every other top-level field passes through unchanged, preserved in source
order and rendered for every target. esheep grants nothing itself; a
passed-through field carries only the meaning the receiving harness gives
it.

Target blocks are esheep configuration, not skill content, and unknown
fields inside them are errors. Every optional claude, pi, codex, or agents
block accepts 'disabled: true'; claude and pi also accept the inert
'argument-hint' string.

Rendering is deterministic. Claude and Pi receive the interpreted fields
and argument-hint when present; Codex and the shared target receive the
interpreted fields without hints; every target receives the passed-through
fields, except that a passed-through field is dropped for a target whose
block renders the same key. When disable-model-invocation is true, the
Codex render also writes agents/openai.yaml containing
'policy.allow_implicit_invocation: false' unless the skill provides that
file itself.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
}
