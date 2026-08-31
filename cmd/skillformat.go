package cmd

import (
	"github.com/spf13/cobra"
)

func newSkillFormatTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "skill-format",
		Short: "Source container layout, SKILL.md frontmatter, and the agents file",
		Long: `Each source is a read-only container: skills are the immediate child
directories of its skills/ directory that contain a manifest, and an
optional global agents file lives at the container root as AGENTS.md or a
profile variant AGENTS.<profile>.md. A container may provide skills, an
agents file, or both; a container without a skills/ directory provides no
skills. Dot-entries and node_modules are skipped. Sources are trusted:
symlinks anywhere beneath a source are followed wherever they resolve. A
link that does not resolve or produces a directory cycle is an error.
Supporting files are validated and rendered as non-executable data, and
supporting paths must be unique under case-insensitive Unicode-normalized
comparison. The skill-root .esheep.toml name is reserved for ownership
metadata.

The agents file is opaque: esheep validates nothing inside it, copies it
byte-identical, and an empty file is legal. Variants share the profile
grammar of SKILL.<profile>.md below, and any other container-root file of
the form AGENTS.<segment>.md is an error.

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
  esheep-targets            Required nonempty list naming where the skill
                            installs: claude, pi, codex, agents. Each entry
                            is a target name, or a single-pair mapping from
                            a target name to a nonempty list of profile
                            names that limits that target to those
                            profiles. Unlisted targets and targets whose
                            profile list matches no active profile are not
                            installed.

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
