package session

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"
)

// Default page limits apply when a search sets neither a limit nor all.
const (
	DefaultHitLimit = 5
	DefaultLimit    = 10
)

// Page selects the part of a search report that is returned. A zero Limit
// returns every remaining session and a zero HitLimit every hit in each; a
// zero After starts at the first session.
type Page struct {
	After    Cursor
	HitLimit int
	Limit    int
}

// Cursor marks the last session of a page in the search order: sort time,
// most recent first, then path.
type Cursor struct {
	Path     string
	SortTime time.Time
}

// ParseCursor decodes a cursor produced by Cursor.String.
func ParseCursor(value string) (Cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, errInvalidCursor
	}
	sortTime, path, found := strings.Cut(string(data), "\n")
	if !found || path == "" {
		return Cursor{}, errInvalidCursor
	}
	parsed, err := time.Parse(time.RFC3339Nano, sortTime)
	if err != nil {
		return Cursor{}, errInvalidCursor
	}
	return Cursor{Path: path, SortTime: parsed}, nil
}

var errInvalidCursor = errors.New("invalid cursor")

// String encodes the cursor as an opaque URL-safe token.
func (c Cursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.SortTime.Format(time.RFC3339Nano) + "\n" + c.Path))
}

// reached reports whether s sorts at or before the cursor position.
func (c Cursor) reached(s Session) bool {
	sortTime := s.SortTime()
	return sortTime.After(c.SortTime) || (sortTime.Equal(c.SortTime) && s.Path <= c.Path)
}

// Paginate returns the page of r that p selects: the sessions after
// p.After, up to p.Limit of them, each with its first p.HitLimit hits.
// MatchedSessions, MatchedHits, and Sources keep describing the whole search.
func (r SearchReport) Paginate(p Page) SearchReport {
	start := 0
	if p.After != (Cursor{}) {
		start = slices.IndexFunc(r.Sessions, func(m Match) bool { return !p.After.reached(m.Session) })
		if start < 0 {
			start = len(r.Sessions)
		}
	}
	end := len(r.Sessions)
	if p.Limit > 0 {
		end = start + min(p.Limit, end-start)
	}

	page := r
	page.Sessions = make([]Match, 0, end-start)
	for _, match := range r.Sessions[start:end] {
		if p.HitLimit > 0 && len(match.Hits) > p.HitLimit {
			match.Hits = match.Hits[:p.HitLimit]
		}
		match.OmittedHits = match.MatchedHits - len(match.Hits)
		page.Sessions = append(page.Sessions, match)
	}
	page.RemainingSessions = len(r.Sessions) - end
	page.HasMore = page.RemainingSessions > 0
	page.NextCursor = nil
	if page.HasMore {
		last := r.Sessions[end-1].Session
		next := Cursor{Path: last.Path, SortTime: last.SortTime()}.String()
		page.NextCursor = &next
	}
	return page
}
