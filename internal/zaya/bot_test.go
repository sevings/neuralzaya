package zaya

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareTextSpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "No special characters",
			input:    "Hello, world",
			expected: []string{"Hello, world"},
		},
		{
			name:     "Single backquote outside triple quotes",
			input:    "`Hello, world!`",
			expected: []string{"`Hello, world!`"},
		},
		{
			name:     "Triple backquote",
			input:    "\n```Hello, world!\n```",
			expected: []string{"\n```Hello, world!\n```"},
		},
		{
			name:     "Escaped backquote inside triple quotes",
			input:    "\n```Hello, `world!`\n```",
			expected: []string{"\n```Hello, \\`world!\\`\n```"},
		},
		{
			name:     "Bold text",
			input:    "**Hello, world!**",
			expected: []string{"*Hello, world\\!*"},
		},
		{
			name:     "Special markdown characters",
			input:    "_Hello, world!_",
			expected: []string{"\\_Hello, world\\!\\_"},
		},
		{
			name:     "Mixed special characters",
			input:    "Hello, `*world*!`",
			expected: []string{"Hello, `*world*!`"},
		},
		{
			name:     "Escaped special characters",
			input:    "\\*Hello, world!*",
			expected: []string{"\\*Hello, world\\!\\*"},
		},
		{
			name:     "Complex example",
			input:    "Hello, **`_world_`**!",
			expected: []string{"Hello, *`_world_`*\\!"},
		},
		{
			name:     "Trailing triple quotes not closed",
			input:    "\n```Hello, world!",
			expected: []string{"\n```Hello, world!\n```"},
		},
		{
			name:     "Trailing backquote not closed",
			input:    "`Hello`, `world!",
			expected: []string{"`Hello`, `world!`"},
		},
		{
			name:     "Trailing bold text not closed",
			input:    "`Hello**`, **world!",
			expected: []string{"`Hello**`, *world\\!*"},
		},
		{
			name:     "Backquote in middle of text",
			input:    "This is `code` in a sentence.",
			expected: []string{"This is `code` in a sentence\\."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prepareMessageText(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestPrepareTextChunking(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		minChunks int
	}{
		{
			name:      "Long text with newlines",
			input:     createLongText(4200, "\n"),
			minChunks: 2,
		},
		{
			name:      "Long text with bold formatting",
			input:     "**" + createLongText(4200, " ") + "**",
			minChunks: 2,
		},
		{
			name:      "Long text with code blocks",
			input:     "`" + createLongText(4200, " ") + "`",
			minChunks: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prepareMessageText(tt.input)
			require.GreaterOrEqual(t, len(result), tt.minChunks, "Expected at least %d chunks, got %d", tt.minChunks, len(result))

			for i, chunk := range result {
				require.LessOrEqual(t, len(chunk), 4000, "Chunk %d exceeds 4000 bytes: %d bytes", i, len(chunk))
			}

			totalLen := 0
			for _, chunk := range result {
				totalLen += len(chunk)
			}
			require.GreaterOrEqual(t, totalLen, len(tt.input), "Total length should be greater than input length")
		})
	}
}

func createLongText(minBytes int, separator string) string {
	baseText := "This is a sample text that will be repeated to create a long string for testing purposes"
	var result string

	for len(result) < minBytes {
		if len(result) > 0 {
			result += separator
		}
		result += baseText
	}

	return result
}
