// Package remote runs esheep on other machines over ssh and merges their
// session reports with the local one.
//
// Each machine answers with the same JSON document its own 'sessions list
// --json' or 'sessions search --json' would print. Only results cross the
// wire; transcripts stay where they are and no state survives a run.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

// Diagnostic codes for machines that could not answer.
const (
	CodeCommand     = "machine-command"
	CodeNoMachines  = "no-machines"
	CodeReply       = "machine-reply"
	CodeTimeout     = "machine-timeout"
	CodeUnreachable = "machine-unreachable"
)

// Selection is the set of machines one command queries. LocalName labels
// local results: the configured entry for this machine when there is one,
// otherwise the first label of the hostname.
type Selection struct {
	Local     bool
	LocalName string
	Machines  []config.ResolvedMachine
}

// Names is a parsed --remote selection, validated as far as possible before
// configuration is loaded.
type Names struct {
	all     bool
	names   []string
	noLocal bool
}

// ParseNames validates the --remote values and --no-local together. Values
// are already split on commas; duplicates collapse case-insensitively.
func ParseNames(values []string, noLocal bool) (Names, error) {
	if len(values) == 0 && noLocal {
		return Names{}, errors.New("--no-local requires --remote")
	}

	parsed := Names{noLocal: noLocal}
	for _, value := range values {
		if strings.EqualFold(value, "all") {
			parsed.all = true
			continue
		}
		if !slices.ContainsFunc(parsed.names, func(name string) bool { return strings.EqualFold(name, value) }) {
			parsed.names = append(parsed.names, value)
		}
	}
	if parsed.all && len(parsed.names) != 0 {
		return Names{}, errors.New("--remote all cannot combine with machine names")
	}
	return parsed, nil
}

// Select resolves the parsed names against the configured machines. The
// entry whose first label matches the first label of hostname is this
// machine: "all" skips it and naming it is an error. Machines keep
// configuration order. A nonfatal diagnostic reports "all" selecting no
// other machine.
func (n Names) Select(machines []config.ResolvedMachine, hostname string) (Selection, []session.Diagnostic, error) {
	localLabel := hostLabel(hostname)
	isSelf := func(machine config.ResolvedMachine) bool { return hostLabel(machine.Name) == localLabel }
	selection := Selection{Local: !n.noLocal, LocalName: localLabel}
	if self := slices.IndexFunc(machines, isSelf); self >= 0 {
		selection.LocalName = machines[self].Name
	}

	if n.all {
		for _, machine := range machines {
			if !isSelf(machine) {
				selection.Machines = append(selection.Machines, machine)
			}
		}
		if len(selection.Machines) != 0 {
			return selection, nil, nil
		}
		if n.noLocal {
			return Selection{}, nil, errors.New("--remote all selected no other machine, so --no-local leaves nothing to search")
		}
		return selection, []session.Diagnostic{{
			Code:    CodeNoMachines,
			Machine: selection.LocalName,
			Message: "--remote all selected no other machine; add [[machines]] entries to esheep.toml",
		}}, nil
	}

	for _, name := range n.names {
		index := slices.IndexFunc(machines, func(machine config.ResolvedMachine) bool { return strings.EqualFold(machine.Name, name) })
		if index < 0 {
			return Selection{}, nil, fmt.Errorf("unknown machine %q (%s)", name, configuredNames(machines))
		}
		if isSelf(machines[index]) {
			return Selection{}, nil, fmt.Errorf("machine %q is this machine; omit it from --remote", name)
		}
	}
	for _, machine := range machines {
		if slices.ContainsFunc(n.names, func(name string) bool { return strings.EqualFold(machine.Name, name) }) {
			selection.Machines = append(selection.Machines, machine)
		}
	}
	return selection, nil, nil
}

func configuredNames(machines []config.ResolvedMachine) string {
	if len(machines) == 0 {
		return "no [[machines]] configured"
	}
	names := make([]string, 0, len(machines))
	for _, machine := range machines {
		names = append(names, machine.Name)
	}
	return "configured: " + strings.Join(names, ", ")
}

// hostLabel is the case-folded first label of a hostname, the identity
// under which machines compare.
func hostLabel(hostname string) string {
	label, _, _ := strings.Cut(hostname, ".")
	return strings.ToLower(label)
}

// List inventories sessions on every selected machine and merges the
// results, most recently started first. ssh is the path to the OpenSSH
// client and is only used when machines are selected.
func List(ctx context.Context, ssh string, selection Selection, request session.Request, local func(context.Context) session.ListReport) (session.ListReport, error) {
	merged, err := run(ctx, ssh, selection, request,
		func(ctx context.Context) document[session.Session] { return document[session.Session](local(ctx)) },
		func(entry *session.Session) *session.Session { return entry })
	return session.ListReport(merged), err
}

// Search searches transcripts on every selected machine and merges the
// matches, most recently started first.
func Search(ctx context.Context, ssh string, selection Selection, request session.Request, local func(context.Context) session.SearchReport) (session.SearchReport, error) {
	merged, err := run(ctx, ssh, selection, request,
		func(ctx context.Context) document[session.Match] { return document[session.Match](local(ctx)) },
		func(entry *session.Match) *session.Session { return &entry.Session })
	return session.SearchReport(merged), err
}

// document is the report shape shared by list and search, parameterized on
// the session entry type so one merge serves both.
type document[S any] struct {
	Complete    bool                 `json:"complete"`
	Diagnostics []session.Diagnostic `json:"diagnostics"`
	Sessions    []S                  `json:"sessions"`
}

// run fans the request out to the local scan and every machine
// concurrently, stamps each part with its machine name, and merges them.
// Completeness is the conjunction of every part; a machine that cannot
// answer contributes one diagnostic and makes the report incomplete.
func run[S any](ctx context.Context, ssh string, selection Selection, request session.Request, local func(context.Context) document[S], describe func(*S) *session.Session) (document[S], error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return document[S]{}, fmt.Errorf("encode request: %w", err)
	}

	parts := make([]document[S], 1+len(selection.Machines))
	var wg sync.WaitGroup
	if selection.Local {
		wg.Go(func() {
			parts[0] = local(ctx)
			stamp(&parts[0], selection.LocalName, describe)
		})
	}
	for index, machine := range selection.Machines {
		wg.Go(func() {
			parts[index+1] = query[S](ctx, ssh, machine, encoded)
			stamp(&parts[index+1], machine.Name, describe)
		})
	}
	wg.Wait()

	merged := document[S]{Complete: true, Diagnostics: []session.Diagnostic{}, Sessions: []S{}}
	for index, part := range parts {
		if index == 0 && !selection.Local {
			continue
		}
		merged.Complete = merged.Complete && part.Complete
		merged.Diagnostics = append(merged.Diagnostics, part.Diagnostics...)
		merged.Sessions = append(merged.Sessions, part.Sessions...)
	}
	slices.SortStableFunc(merged.Sessions, func(left, right S) int {
		l, r := describe(&left), describe(&right)
		if order := r.SortTime().Compare(l.SortTime()); order != 0 {
			return order
		}
		if order := strings.Compare(l.Machine, r.Machine); order != 0 {
			return order
		}
		return strings.Compare(l.Path, r.Path)
	})
	return merged, nil
}

func stamp[S any](part *document[S], machine string, describe func(*S) *session.Session) {
	for index := range part.Sessions {
		describe(&part.Sessions[index]).Machine = machine
	}
	for index := range part.Diagnostics {
		part.Diagnostics[index].Machine = machine
	}
}

// query runs 'esheep sessions query' on one machine over ssh with the
// encoded request on stdin and decodes the reply. Any failure becomes one
// diagnostic and an incomplete part.
func query[S any](ctx context.Context, ssh string, machine config.ResolvedMachine, request []byte) document[S] {
	ctx, cancel := context.WithTimeout(ctx, machine.Timeout)
	defer cancel()
	// -T overrides a RequestTTY setting in ssh config; a terminal would echo
	// the request and merge stderr into the JSON reply.
	command := exec.CommandContext(ctx, ssh,
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+strconv.Itoa(connectTimeoutSeconds(machine.Timeout)),
		machine.Host,
		machine.Command+" sessions query",
	)
	var stdout, stderr bytes.Buffer
	command.Stdin = bytes.NewReader(request)
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.WaitDelay = time.Second
	runErr := command.Run()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return failure[S](CodeTimeout, fmt.Sprintf("no reply within %s", machine.Timeout))
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) && exit.ExitCode() == sshFailureExit {
		return failure[S](CodeUnreachable, lastLines(stderr.String(), runErr))
	}
	var reply document[S]
	if decodeErr := json.Unmarshal(stdout.Bytes(), &reply); decodeErr != nil {
		if runErr != nil {
			return failure[S](CodeCommand, lastLines(stderr.String(), runErr))
		}
		return failure[S](CodeReply, fmt.Sprintf("%s; stdout begins %q", decodeErr, leading(stdout.Bytes())))
	}
	return reply
}

// sshFailureExit is the status OpenSSH uses for its own failures: name
// resolution, connection, authentication, and host-key rejection.
const sshFailureExit = 255

func failure[S any](code, message string) document[S] {
	return document[S]{Diagnostics: []session.Diagnostic{{Code: code, Message: message}}}
}

// connectTimeoutSeconds bounds ssh's own connection phase to the smaller
// of ten seconds and the machine's timeout, in the whole seconds ssh accepts.
func connectTimeoutSeconds(timeout time.Duration) int {
	const ceiling = 10
	return max(1, min(ceiling, int(math.Ceil(timeout.Seconds()))))
}

// lastLines keeps the tail of a process's stderr for a diagnostic message,
// falling back to the run error when the process said nothing.
func lastLines(stderr string, runErr error) string {
	const keep = 5
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return runErr.Error()
	}
	return strings.Join(lines[max(0, len(lines)-keep):], "; ")
}

func leading(data []byte) string {
	const width = 80
	return string(data[:min(width, len(data))])
}
