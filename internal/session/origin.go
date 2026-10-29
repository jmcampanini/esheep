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

// Valid reports whether k names a supported event kind.
func (k EventKind) Valid() bool {
	return k == KindMessage || k == KindRaw || k == KindToolCall || k == KindToolResult
}

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

// Valid reports whether k names a supported session origin.
func (k OriginKind) Valid() bool {
	return k == OriginAgent || k == OriginAutomation || k == OriginHuman || k == OriginSubagent || k == OriginUnknown
}

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

// Valid reports whether k names a supported event origin.
func (k EventOriginKind) Valid() bool {
	return k == EventOriginAgent || k == EventOriginHarness || k == EventOriginHuman || k == EventOriginTool || k == EventOriginUnknown
}

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

func originEvidence(roots map[string]json.RawMessage) map[string]string {
	evidence := make(map[string]string)
	for _, key := range slices.Sorted(maps.Keys(roots)) {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(roots[key]))
		decoder.UseNumber()
		if decoder.Decode(&value) == nil {
			appendOriginEvidence(evidence, key, value, 0)
		}
	}
	return evidence
}

func appendOriginEvidence(evidence map[string]string, key string, value any, depth int) {
	if len(evidence) >= 32 || len(key) > 256 {
		return
	}
	switch value := value.(type) {
	case string:
		evidence[key] = boundedEvidenceValue(value)
	case json.Number:
		evidence[key] = boundedEvidenceValue(string(value))
	case map[string]any:
		if depth >= 4 {
			return
		}
		for _, child := range slices.Sorted(maps.Keys(value)) {
			appendOriginEvidence(evidence, key+"."+child, value[child], depth+1)
		}
	}
}

func boundedEvidenceValue(value string) string {
	if len(value) <= 256 {
		return value
	}
	end := 253
	for !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "..."
}
