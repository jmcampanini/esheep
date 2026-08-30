package cmd

import (
	"github.com/spf13/cobra"
)

func newSkillFormatTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "skill-format",
		Short: "Skill directory layout and SKILL.md frontmatter",
		Long: `Each source is a read-only collection of top-level skill directories: an
immediate child directory containing a manifest is a skill. Dot-directories,
node_modules, and symlinked children are skipped. Supporting files are
validated and rendered as non-executable data; supporting paths must be
unique under case-insensitive Unicode-normalized comparison, and absolute,
escaping, cyclic, and directory symlinks are rejected. The root .esheep.toml
name is reserved for ownership metadata.

Manifests are SKILL.md and profile variants named SKILL.<profile>.md, where
<profile> is 1-64 characters of lowercase alphanumeric words separated by
single hyphens; base and local are reserved names. Any other root file of
the form SKILL.<segment>.md is an error. SKILL.md applies under every
profile unless esheep-only-profiles narrows it; SKILL.<profile>.md applies
only under its filename profile unioned with esheep-only-profiles. Under
the active profiles, an applying profile variant overrides SKILL.md, and
two applying variants are a conflict that blocks synchronization. The
selected manifest always renders as SKILL.md.

A manifest is YAML frontmatter followed by a Markdown body preserved
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
  esheep-only-profiles      Optional nonempty list of profile names. Limits
                            the manifest to the named profiles.
  esheep-claude-disabled    Optional booleans, one per target. When true,
  esheep-pi-disabled        the skill is not installed for that target.
  esheep-codex-disabled
  esheep-agents-disabled

The esheep- key prefix is reserved: interpreted esheep- keys configure
esheep and are never rendered, and any other esheep- key is an error.
Every other top-level field passes through unchanged, preserved in source
order and rendered for every target. esheep grants nothing itself; a
passed-through field carries only the meaning the receiving harness gives
it.

Rendering is deterministic. Every target receives the interpreted content
fields and the passed-through fields. When disable-model-invocation is
true, the Codex render also writes agents/openai.yaml containing
'policy.allow_implicit_invocation: false' unless the skill provides that
file itself.`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
}
