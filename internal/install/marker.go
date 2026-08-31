// Package install inspects and synchronizes owned target skill directories.
package install

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/jmcampanini/esheep/internal/naming"
	"github.com/jmcampanini/esheep/internal/render"
	"github.com/jmcampanini/esheep/internal/skill"
)

// MarkerName is the reserved ownership marker filename.
const MarkerName = ".esheep.toml"

// Marker identifies one esheep-owned installation.
type Marker struct {
	Skill  string        `toml:"skill" json:"skill"`
	Source string        `toml:"source" json:"source"`
	Target render.Target `toml:"target" json:"target"`
}

// ParseMarker parses and validates strict ownership metadata.
func ParseMarker(data []byte) (Marker, error) {
	var decoded struct {
		Skill  string `toml:"skill"`
		Source string `toml:"source"`
		Target string `toml:"target"`
	}
	metadata, err := toml.Decode(string(data), &decoded)
	if err != nil {
		return Marker{}, fmt.Errorf("parse ownership marker: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		return Marker{}, fmt.Errorf("parse ownership marker: unknown field %q", undecoded[0].String())
	}
	if err := naming.ValidateSourceName(decoded.Source); err != nil {
		return Marker{}, fmt.Errorf("parse ownership marker: %w", err)
	}
	if !skill.ValidIdentity(decoded.Skill, decoded.Skill) {
		return Marker{}, fmt.Errorf("parse ownership marker: invalid skill %q", decoded.Skill)
	}
	target := render.Target(decoded.Target)
	if !validTarget(target) {
		return Marker{}, fmt.Errorf("parse ownership marker: invalid target %q", decoded.Target)
	}

	return Marker{Skill: decoded.Skill, Source: decoded.Source, Target: target}, nil
}

// MarshalMarker returns canonical ownership metadata.
func MarshalMarker(marker Marker) ([]byte, error) {
	if err := naming.ValidateSourceName(marker.Source); err != nil {
		return nil, fmt.Errorf("marshal ownership marker: %w", err)
	}
	if !skill.ValidIdentity(marker.Skill, marker.Skill) {
		return nil, fmt.Errorf("marshal ownership marker: invalid skill %q", marker.Skill)
	}
	if !validTarget(marker.Target) {
		return nil, fmt.Errorf("marshal ownership marker: invalid target %q", marker.Target)
	}

	text := fmt.Sprintf("source = %q\nskill = %q\ntarget = %q\n", marker.Source, marker.Skill, marker.Target)
	return []byte(text), nil
}

func validTarget(target render.Target) bool {
	switch target {
	case render.TargetClaude, render.TargetCodex, render.TargetPi:
		return true
	default:
		return false
	}
}
