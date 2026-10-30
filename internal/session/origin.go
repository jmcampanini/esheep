package session

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"unicode/utf8"
)

// EventKind distinguishes messages, invocations, results, and undecoded lines.
type EventKind string

// Supported event kinds.
const (
	KindMessage    EventKind = "message"
	KindRaw        EventKind = "raw"
	KindToolCall   EventKind = "tool_call"
	KindToolResult EventKind = "tool_result"
)

// OriginKind classifies recorded session initiation.
type OriginKind string

// Supported session origins. Human means recorded user initiation.
const (
	OriginAgent      OriginKind = "agent"
	OriginAutomation OriginKind = "automation"
	OriginHuman      OriginKind = "human"
	OriginSubagent   OriginKind = "subagent"
	OriginUnknown    OriginKind = "unknown"
)

// EventOriginKind classifies the recorded producer of an event.
type EventOriginKind string

// Supported event origins. Tool means the recorded tool-result channel.
const (
	EventOriginAgent   EventOriginKind = "agent"
	EventOriginHarness EventOriginKind = "harness"
	EventOriginHuman   EventOriginKind = "human"
	EventOriginTool    EventOriginKind = "tool"
	EventOriginUnknown EventOriginKind = "unknown"
)

// Origin separates recorded initiation from the authorship of events.
// Evidence contains bounded scalar metadata keyed by native field path.
type Origin struct {
	Evidence map[string]string `json:"evidence,omitempty"`
	Kind     OriginKind        `json:"kind"`
}

// EventOrigin describes an event independently of its session's initiation.
// Evidence contains bounded scalar metadata keyed by native field path.
type EventOrigin struct {
	Evidence map[string]string `json:"evidence,omitempty"`
	Kind     EventOriginKind   `json:"kind"`
}

func transcriptOrigin(subagent bool) Origin {
	kind := OriginUnknown
	if subagent {
		kind = OriginSubagent
	}
	return Origin{Kind: kind}
}

const (
	evidenceMaxDepth   = 4
	evidenceMaxEntries = 32
	evidenceMaxKey     = 256
	evidenceMaxValue   = 256
)

func originEvidence(roots map[string]json.RawMessage) map[string]string {
	evidence := make(map[string]string)
	for _, key := range slices.Sorted(maps.Keys(roots)) {
		appendOriginEvidence(evidence, key, roots[key], 0)
	}
	return evidence
}

func appendOriginEvidence(evidence map[string]string, key string, raw json.RawMessage, depth int) {
	if len(evidence) >= evidenceMaxEntries || len(key) > evidenceMaxKey {
		return
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		if value, ok := scalarEvidence(raw); ok {
			evidence[key] = value
		}
		return
	}
	if depth >= evidenceMaxDepth {
		return
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return
	}
	for _, child := range slices.Sorted(maps.Keys(members)) {
		appendOriginEvidence(evidence, key+"."+child, members[child], depth+1)
	}
}

// scalarEvidence returns the bounded text of a JSON string or number,
// preserving the recorded spelling of numbers.
func scalarEvidence(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	switch raw[0] {
	case '"':
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", false
		}
		return boundedEvidenceValue(value), true
	case '{', '[', 'n', 't', 'f':
		return "", false
	default:
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", false
		}
		return boundedEvidenceValue(string(number)), true
	}
}

func boundedEvidenceValue(value string) string {
	if len(value) <= evidenceMaxValue {
		return value
	}
	end := evidenceMaxValue - len("...")
	for !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "..."
}
