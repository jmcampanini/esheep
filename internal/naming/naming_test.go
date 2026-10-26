package naming

import "testing"

func TestValidateLayerNameAndSegment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		layer        string
		validName    bool
		validSegment bool
	}{
		{name: "single word", layer: "work", validName: true, validSegment: true},
		{name: "hyphenated", layer: "work-laptop", validName: true, validSegment: true},
		{name: "numeric", layer: "2024", validName: true, validSegment: true},
		{name: "empty", layer: ""},
		{name: "uppercase", layer: "Work"},
		{name: "dot", layer: "work.layer"},
		{name: "trailing hyphen", layer: "work-"},
		{name: "reserved base", layer: "base"},
		{name: "automatic local", layer: "local", validSegment: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateLayerName(test.layer); (err == nil) != test.validName {
				t.Errorf("ValidateLayerName(%q) = %v, want valid %v", test.layer, err, test.validName)
			}
			if err := ValidateLayerSegment(test.layer); (err == nil) != test.validSegment {
				t.Errorf("ValidateLayerSegment(%q) = %v, want valid %v", test.layer, err, test.validSegment)
			}
		})
	}
}
