package zaya

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEscapeSpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No special characters",
			input:    "Hello, world",
			expected: "Hello, world",
		},
		{
			name:     "Single backquote outside triple quotes",
			input:    "`Hello, world!`",
			expected: "`Hello, world!`",
		},
		{
			name:     "Triple backquote",
			input:    "\n```Hello, world!\n```",
			expected: "\n```Hello, world!\n```",
		},
		{
			name:     "Escaped backquote inside triple quotes",
			input:    "\n```Hello, `world!`\n```",
			expected: "\n```Hello, \\`world!\\`\n```",
		},
		{
			name:     "Bold text",
			input:    "**Hello, world!**",
			expected: "*Hello, world\\!*",
		},
		{
			name:     "Special markdown characters",
			input:    "_Hello, world!_",
			expected: "\\_Hello, world\\!\\_",
		},
		{
			name:     "Mixed special characters",
			input:    "Hello, `*world*!`",
			expected: "Hello, `*world*!`",
		},
		{
			name:     "Escaped special characters",
			input:    "\\*Hello, world!*",
			expected: "\\*Hello, world\\!\\*",
		},
		{
			name:     "Complex example",
			input:    "Hello, **`_world_`**!",
			expected: "Hello, *`_world_`*\\!",
		},
		{
			name:     "Trailing triple quotes not closed",
			input:    "\n```Hello, world!",
			expected: "\n```Hello, world!\n```",
		},
		{
			name:     "Trailing backquote not closed",
			input:    "`Hello`, `world!",
			expected: "`Hello`, `world!`",
		},
		{
			name:     "Trailing bold text not closed",
			input:    "`Hello**`, **world!",
			expected: "`Hello**`, *world\\!*",
		},
		{
			name:     "Backquote in middle of text",
			input:    "This is `code` in a sentence.",
			expected: "This is `code` in a sentence\\.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeSpecialChars(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
