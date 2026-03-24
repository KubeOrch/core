package routes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAllowedOrigins(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string returns default",
			input:    "",
			expected: []string{"http://localhost:3001"},
		},
		{
			name:     "single origin",
			input:    "https://app.example.com",
			expected: []string{"https://app.example.com"},
		},
		{
			name:  "multiple origins comma separated",
			input: "https://app.example.com,https://admin.example.com",
			expected: []string{
				"https://app.example.com",
				"https://admin.example.com",
			},
		},
		{
			name:  "trims whitespace",
			input: " https://a.com , https://b.com ",
			expected: []string{
				"https://a.com",
				"https://b.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAllowedOrigins(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
