package cmd

import (
	"context"
	"fmt"

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

Deterministic action rows and the final summary are payload on stdout. The
command continues unrelated work after individual skill or target failures
and exits 1 when the final summary contains failures.

Every managed skill installation carries a .esheep.toml ownership marker
recording its source, skill, and target. Synchronization never modifies an
unmarked, mismatched, or symlinked destination, prunes only validly marked
stale output, and leaves disabled targets entirely untouched. Replacements
are staged on the target filesystem and committed atomically; invalid,
colliding, conflicting, or unavailable configured source skills protect
existing output. Skills whose profiles are not active are reported
inactive and their previously installed output is pruned.

The global agents file has positional ownership instead of a marker.
Selection walks the active profiles in order and then the unprofiled name:
each tier may match at most one AGENTS.<profile>.md or AGENTS.md across
all sources, the first tier with exactly one file wins, an empty tier
falls through, and a tier with several files is an error that blocks
agents file synchronization. The selected file is copied byte-identical to
every enabled target's agents_md_path, atomically, overwriting whatever
occupies that path. esheep never deletes a deployed agents file: when no
source provides one, destinations are left untouched, and selection is
skipped entirely while any configured source is unavailable.

Serialize invocations: concurrent mutating esheep commands against the
same targets are unsupported.

` + streamContractHelp,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			loaded, err := loadConfiguration(command, load)
			if err != nil {
				return err
			}
			report := synchronize(command.Context(), loaded)
			if err := ui.WriteSync(command.OutOrStdout(), report, ui.ShouldColor(command.OutOrStdout())); err != nil {
				return appError(err)
			}
			if err := ui.WriteDiagnostics(command.ErrOrStderr(), report.Diagnostics); err != nil {
				return appError(err)
			}
			if report.Summary.Failed != 0 {
				return appError(fmt.Errorf("sync completed with %d failure(s)", report.Summary.Failed))
			}
			return nil
		},
	}
}
