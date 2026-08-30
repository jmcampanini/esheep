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
