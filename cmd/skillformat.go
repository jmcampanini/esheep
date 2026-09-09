package cmd

import (
	"fmt"

	"github.com/jmcampanini/esheep/internal/expansion"
	"github.com/spf13/cobra"
)

func newSkillFormatTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "skill-format",
		Short: "Source container layout, SKILL.md frontmatter, and the agents file",
		Long: fmt.Sprintf(`Each source is a read-only container: skills are the immediate child
directories of its skills/ directory that contain a manifest, and an
optional global agents file lives in the sibling agents-md/ directory as
AGENTS.md or a profile variant AGENTS.<profile>.md. A container may
provide skills, an agents file, or both; a container without a skills/
directory provides no skills, and one without an agents-md/ directory
provides no agents file. Container-root files, including a
repository-local AGENTS.md, are ignored: only agents-md/ holds managed
instruction files. Skill discovery skips immediate children of skills/
whose names start with '.' or equal node_modules.

Within each skill, files and directories whose names start with '.' are
skipped at every depth before validation or copying. Hidden directories
are not traversed, and hidden symlinks are not resolved. The exception is
the skill-root .esheep.toml name, which is reserved case-insensitively for
ownership metadata and is an error in a source skill.
Visible directories remain even when all their contents are skipped.

Within each skill, files and directories with the case-sensitive esheep-
prefix are source-only at every depth. They and all descendants remain
subject to source validation but are never copied into an installation.
Harness include files use this prefix and stay in the owning root: the
skill directory for skills or the source's agents-md/ directory for agents
files. Agents include fragments are source-only and never deployed.

Sources are trusted: included symlinks are followed wherever they resolve,
even when the resolved path contains hidden names. An included link that
does not resolve or produces a directory cycle is an error.
Supporting files are validated and rendered as non-executable data, and
supporting paths must be unique under case-insensitive Unicode-normalized
comparison.

The entire selected agents file uses the same variable language as skill
bodies, including text that resembles frontmatter. Agents files have no
interpreted frontmatter. Content outside substitutions stays byte-identical.
An empty rendered agents file is legal and is installed. Variants share the
profile grammar of SKILL.<profile>.md below, and any other agents-md/ file
of the form AGENTS.<segment>.md is an error.

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
byte-for-byte except esheep variables, described below. Frontmatter fields
fall into two categories: fields esheep interprets and validates, and
fields it passes through verbatim.

Interpreted fields:

  name                      Required. 1-64 characters, lowercase
                            alphanumeric words separated by single hyphens,
                            equal to the skill directory name.
  esheep-trigger            Required nonblank string. At most 1024 Unicode
                            characters. States when to invoke the skill;
                            renders as description for every target.
  license                   Optional string.
  compatibility             Optional string. At most 500 Unicode characters.
  metadata                  Optional string-to-string map.
  disable-model-invocation  Optional boolean. When true, rendered output
                            tells each target not to invoke the skill
                            automatically.
  esheep-disabled           Optional boolean, default false. When true on
                            the selected manifest, prevents installation
                            on every target.
  esheep-only-profiles      Optional nonempty list of profile names. Limits
                            the manifest to the named profiles.
  esheep-targets            Required nonempty list naming where the skill
                            installs: claude, pi, codex. Each entry is a
                            target name, or a single-pair mapping from a
                            target name to a nonempty list of profile
                            names that limits that target to those
                            profiles. Unlisted targets and targets whose
                            profile list matches no active profile are not
                            installed.

esheep-disabled applies only after profile selection. A profile variant
does not inherit the base SKILL.md flag, and a selected disabled variant
does not fall back to SKILL.md. Remove the flag or set it to false to
re-enable that manifest subject to its target and profile gates.
Disabling preserves discovery and validation, including required fields,
supporting files, name collisions, and profile conflicts. Target-specific
includes are not expanded for a disabled manifest.

List and status report disabled readiness for a valid selected disabled
manifest; invalid, collision, and conflict take precedence. On the next
sync, previously managed copies are pruned from enabled targets using the
normal ownership protections. Disabled targets remain untouched. Status
treats disabled as healthy without checking whether an individual copy
still exists; sync performs removal. Unlike esheep-disabled,
disable-model-invocation still installs the skill.

The top-level description field is reserved case-insensitively for rendered
output and is an error in every source manifest, including profile variants.
The esheep- key prefix is reserved. Interpreted esheep- keys are never
emitted under their source names; any other esheep- key is an error.
Every other top-level field passes through unchanged, preserved in source
order and rendered for every target. esheep grants nothing itself; a
passed-through field carries only the meaning the receiving harness gives
it.

The {{esheep. text is reserved everywhere in manifest bodies, entire managed
AGENTS.md and AGENTS.<profile>.md files, and included files: every occurrence
must be exactly a known variable, a known variable must occupy its own line
without indentation, and there is no escape syntax. Anything else fails
validation. Rendering substitutes each variable with its value, preserving
all other bytes, including surrounding line endings. Included files are
text without interpreted frontmatter. Skill frontmatter and copied
supporting files are never substituted. When a variable or an included file
changes rendered output, that target's installation drifts and the next sync
repairs it. Unused include files do not affect installed output.

Variables for skill bodies and agents files:

  {{esheep.sources}}        Replaced with a Markdown bullet list of the
                            configured source container roots as resolved
                            absolute paths, one per line, in
                            configuration order.

  {{esheep.include-by-harness "body"}}
                            Insert the required esheep-body-<harness>.md
                            from the owning root, using claude, pi, or codex
                            for the current installation harness.

  {{esheep.include-by-harness-optional "extras"}}
                            Insert esheep-extras-<harness>.md from the owning
                            root. A genuinely absent file quietly inserts
                            zero bytes, with no fallback to another file.
                            Surrounding line endings remain unchanged.

Both include directives accept a 1-64 character lowercase name using the
skill name grammar. esheep adds the esheep- prefix, harness suffix, and .md
extension. Paths are not accepted. The owning root is the skill directory
or the selected source's agents-md/ directory. Every nested include uses
that same root and harness, even inside a symlinked fragment.

Variables in included files expand recursively. Include cycles, including
symlink aliases, fail immediately. At most %d included-file levels are
allowed; the selected manifest or agents file is level zero. Repeated
includes outside the active include chain are allowed. Source-list values
are literal paths and are not expanded again. There are no template
expressions or executable hooks.

Includes expand only for enabled targets and, for skills, only where the
selected manifest applies. A missing required include fails rendering.
Optional includes permit only genuinely absent files: broken symlinks,
unreadable or nonregular files, malformed variables, cycles, and depth
violations fail for both directives. A target-specific rendering failure
preserves that destination. Synchronization continues unrelated work and
exits nonzero on failure. Status compares each destination against its
per-target rendered bytes. Agents-file rendering errors report blocked even
when the destination is absent; absent skill installations report missing.
An empty rendered agents file is installed; an empty rendered skill body
retains its frontmatter.

For one personal skill with different Pi and Codex instructions, use:

  skills/fable-review/SKILL.personal.md
  skills/fable-review/esheep-body-pi.md
  skills/fable-review/esheep-body-codex.md

Keep name: fable-review, esheep-trigger describing when to invoke the skill,
and esheep-targets: [pi, codex] in the manifest. Place
{{esheep.include-by-harness "body"}} on its own body line. Both installations
receive the same name and shared frontmatter, their expanded body, and
ordinary supporting files. The include files are not installed.

Rendering is deterministic. Every target receives the interpreted content
fields and the passed-through fields. When disable-model-invocation is
true, the Codex render also writes agents/openai.yaml containing
'policy.allow_implicit_invocation: false' unless the skill provides that
file itself.`, expansion.MaxIncludeDepth),
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
}
