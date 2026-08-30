package naming

import "testing"

func TestValidateProfileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		profile string
		valid   bool
	}{
		{name: "single word", profile: "work", valid: true},
		{name: "hyphenated", profile: "work-laptop", valid: true},
		{name: "numeric", profile: "2024", valid: true},
		{name: "empty", profile: "", valid: false},
		{name: "uppercase", profile: "Work", valid: false},
		{name: "dot", profile: "work.laptop", valid: false},
		{name: "trailing hyphen", profile: "work-", valid: false},
		{name: "reserved base", profile: "base", valid: false},
		{name: "reserved local", profile: "local", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProfileName(test.profile)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateProfileName(%q) = %v, want valid %v", test.profile, err, test.valid)
			}
		})
	}
}
