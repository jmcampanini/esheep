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

var sourceNamePart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const maxProfileNameLength = 64

// ValidateProfileName validates a profile identity. Profile names share the
// skill-name grammar so they can appear as manifest filename segments, and the
// base and local names are reserved for cross-tool layer semantics.
func ValidateProfileName(name string) error {
	if name == "" || len(name) > maxProfileNameLength || !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	if name == "base" || name == "local" {
		return fmt.Errorf("profile name %q is reserved", name)
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
