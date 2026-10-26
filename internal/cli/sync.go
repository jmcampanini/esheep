package cli

import (
	"context"
	"fmt"

	"github.com/jmcampanini/esheep/internal/apperr"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/ui"
	"github.com/spf13/cobra"
)

func newSyncCommand(load configLoader, synchronize func(context.Context, config.LoadResult) manage.SyncReport) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Synchronize enabled skill targets",
		Long: `Install, repair, and prune esheep-owned output on enabled targets.

The command writes only at each enabled target's skills_path and
agents_md_path. It creates the missing parent directories of both, then
creates, replaces, and deletes one directory per managed skill under
skills_path and replaces the agents_md_path file as a whole, staging the
new content in a temporary .esheep-agents-md-* file beside it. Pruning
deletes managed skill directories under skills_path whose source skill is
gone, inactive, or disabled. 'esheep config' prints the resolved paths.
Sources are read in place and never written.

Deterministic action rows and the final summary are payload on stdout. The
command continues unrelated work after individual skill or target failures
and exits 1 when the final summary contains failures.

Every managed skill installation carries a .esheep.toml ownership marker
recording its source, skill, and target. Synchronization never modifies an
unmarked, mismatched, or symlinked destination, prunes only validly marked
stale output, and leaves disabled targets entirely untouched. Replacements
are staged on the target filesystem and committed atomically; invalid,
colliding, or unavailable configured source skills protect existing
output. Skills with no manifest applying under the effective layers are
reported inactive and their previously installed output is pruned.
When the selected manifest declares esheep-disabled: true, the skill is
reported disabled and its previously managed copies are pruned from
enabled targets. Disabling does not bypass source validation or ownership
protections, and disabled targets remain untouched.

The global agents file has positional ownership instead of a marker.
Candidates are the AGENTS.md, AGENTS.<layer>.md, and AGENTS.local.md
files inside each source's agents-md/ directory; container-root files are
ignored. Selection walks the layer order: AGENTS.md as the base layer, the
effective layers in order, then AGENTS.local.md as the automatic local
layer. The last tier with a file wins and an empty tier contributes
nothing, so AGENTS.local.md overrides every other candidate whenever it
is present. Each tier in the order may hold at most one file across all
sources; a tier with several files is an error that blocks agents file
synchronization even when a later tier would win. Files for layers
outside the order are ignored.

The selected file is rendered for every enabled target and installed
atomically at its non-symlink agents_md_path, overwriting any existing
regular file there. The entire agents file uses the same variables as skill
bodies, without interpreted frontmatter. Includes resolve from the selected
source's agents-md/ directory and their fragments are never deployed.
An absent optional include inserts zero bytes without fallback; empty
rendered agents files are installed. Other bytes, including surrounding
line endings, remain unchanged. See 'esheep help skill-format' for the
complete variable and include contract.

A target-specific rendering failure preserves that destination; unrelated
synchronization continues and the command exits nonzero. Includes are
expanded only for enabled targets. esheep never deletes a deployed agents
file: when no source provides one, destinations are left untouched, and
selection is skipped entirely while any configured source is unavailable.

Serialize invocations: concurrent mutating esheep commands against the
same targets are unsupported.

` + ui.StreamContractHelp + `

` + ui.ColorContractHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := synchronize(command.Context(), loaded)
			if err := ui.WriteSync(command.OutOrStdout(), report, useColor(command, command.OutOrStdout())); err != nil {
				return apperr.Wrap(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return apperr.Wrap(err)
			}
			if report.Summary.Failed != 0 {
				return apperr.Wrap(fmt.Errorf("sync completed with %d failure(s)", report.Summary.Failed))
			}
			return nil
		},
	}
}
