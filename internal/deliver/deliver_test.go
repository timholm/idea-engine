package deliver

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"code-navigator", "code-navigator"},
		{"Code Navigator", "code-navigator"},
		{"test@product#1", "test-product-1"},
		{"  spaces  ", "spaces"},
		{"UPPER-CASE", "upper-case"},
		{"multiple---dashes", "multiple-dashes"},
		{"trailing-.", "trailing"},
	}

	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
