// Package naming holds the shared identity grammar and comparison rules that
// source configuration, skill validation, and target installation must agree on.
package naming

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Automatic layer names. BaseLayer is the layer of a source file without a
// layer suffix and LocalLayer is the layer of a .local variant; both apply
// without being selected and are never effective layers.
const (
	BaseLayer  = "base"
	LocalLayer = "local"
)

var sourceNamePart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const maxSkillNameLength = 64

// ValidSkillName reports whether name follows the grammar shared by skill
// identities, include names, and layer names before their reserved-name check.
func ValidSkillName(name string) bool {
	return len(name) <= maxSkillNameLength && skillNamePattern.MatchString(name)
}

// ValidateLayerName validates a selected layer name or a gate entry. Layer
// names share the skill-name grammar so they can appear as filename segments,
// and the automatic base and local layers cannot be selected or gated on.
func ValidateLayerName(name string) error {
	if err := ValidateLayerSegment(name); err != nil {
		return err
	}
	if name == LocalLayer {
		return fmt.Errorf("layer name %q is reserved", name)
	}
	return nil
}

// ValidateLayerSegment validates the layer segment of a variant filename such
// as SKILL.<layer>.md. The local segment names the automatic local layer; the
// base segment is rejected because the file without a segment is the base
// layer.
func ValidateLayerSegment(name string) error {
	if !ValidSkillName(name) {
		return fmt.Errorf("invalid layer name %q", name)
	}
	if name == BaseLayer {
		return fmt.Errorf("layer name %q is reserved", name)
	}
	return nil
}

// ValidateSourceName validates a configured source identity.
func ValidateSourceName(name string) error {
	if name == "" || len(name) > 255 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid source name %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || !sourceNamePart.MatchString(part) {
			return fmt.Errorf("invalid source name %q", name)
		}
	}
	return nil
}

// PathKey returns the case-insensitive Unicode-normalized key under which
// paths and path components compare for collision and ownership decisions.
func PathKey(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}
