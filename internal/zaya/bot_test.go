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

func TestPrepareTextActionButtons(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedText     []string
		expectedKeyboard bool
		expectedButtons  int
	}{
		{
			name:             "No actions",
			input:            "Regular text without actions",
			expectedText:     []string{"Regular text without actions"},
			expectedKeyboard: false,
			expectedButtons:  0,
		},
		{
			name:             "Single action",
			input:            "Some text <zaya_action>Walk to the door</zaya_action> more text",
			expectedText:     []string{"Some text 1\\. Walk to the door more text"},
			expectedKeyboard: true,
			expectedButtons:  1,
		},
		{
			name:             "Multiple actions",
			input:            "Text <zaya_action>Walk north</zaya_action> and <zaya_action>Use key</zaya_action> and <zaya_action>Read scroll</zaya_action> end",
			expectedText:     []string{"Text 1\\. Walk north and 2\\. Use key and 3\\. Read scroll end"},
			expectedKeyboard: true,
			expectedButtons:  3,
		},
		{
			name:             "Action at end",
			input:            "Choose your action: <zaya_action>Attack the dragon</zaya_action>",
			expectedText:     []string{"Choose your action: 1\\. Attack the dragon"},
			expectedKeyboard: true,
			expectedButtons:  1,
		},
		{
			name:             "Invalid action format",
			input:            "Text <zaya_action>Invalid format<zaya_action> more text",
			expectedText:     []string{"Text <zaya\\_action\\>Invalid format<zaya\\_action\\> more text"},
			expectedKeyboard: false,
			expectedButtons:  0,
		},
		{
			name:             "Empty action description",
			input:            "<zaya_action></zaya_action>",
			expectedText:     []string{"<zaya\\_action\\></zaya\\_action\\>"},
			expectedKeyboard: false,
			expectedButtons:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedKeyboard {
				// Create a bot instance for tests that expect keyboards
				bot := &Bot{}
				result, keyboard := bot.prepareMessageTextWithActions(tt.input)
				require.Equal(t, tt.expectedText, result, "Text chunks should match")
				require.NotNil(t, keyboard, "Keyboard should not be nil")
				require.NotNil(t, keyboard.InlineKeyboard, "InlineKeyboard should not be nil")
				require.Len(t, keyboard.InlineKeyboard, 1, "Should have one row")
				require.Len(t, keyboard.InlineKeyboard[0], tt.expectedButtons, "Should have expected number of buttons")
			} else {
				// Use the regular function for non-action tests
				result := prepareMessageText(tt.input)
				require.Equal(t, tt.expectedText, result, "Text chunks should match")
			}
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

func TestActionButtonFullFlow(t *testing.T) {
	// Test the complete action button flow with realistic AI response
	aiResponse := `Here are your options:

<zaya_action>Walk north</zaya_action>
<zaya_action>Use key</zaya_action>
<zaya_action>Light torch</zaya_action>
<zaya_action>Attack</zaya_action>

Choose wisely.`

	// Create a bot instance with minimal setup
	bot := &Bot{}

	result, keyboard := bot.prepareMessageTextWithActions(aiResponse)

	// Verify text processing
	require.Len(t, result, 1, "Should have one text chunk")
	require.Contains(t, result[0], "Here are your options:", "Should contain intro text")
	require.Contains(t, result[0], "Choose wisely", "Should contain closing text")
	require.NotContains(t, result[0], "<zaya_action>", "Should not contain action patterns")
	require.Contains(t, result[0], "1\\. Walk north", "Should contain numbered action 1")
	require.Contains(t, result[0], "2\\. Use key", "Should contain numbered action 2")
	require.Contains(t, result[0], "3\\. Light torch", "Should contain numbered action 3")
	require.Contains(t, result[0], "4\\. Attack", "Should contain numbered action 4")

	// Verify keyboard creation
	require.NotNil(t, keyboard, "Keyboard should be created")
	require.NotNil(t, keyboard.InlineKeyboard, "InlineKeyboard should exist")
	require.Len(t, keyboard.InlineKeyboard, 1, "Should have one row")
	require.Len(t, keyboard.InlineKeyboard[0], 4, "Should have 4 action buttons")

	// Verify button data
	buttons := keyboard.InlineKeyboard[0]
	expectedButtons := []struct {
		text string
	}{
		{"1"},
		{"2"},
		{"3"},
		{"4"},
	}

	for i, expectedBtn := range expectedButtons {
		require.Equal(t, expectedBtn.text, buttons[i].Text, "Button %d text should match", i)
	}
}
