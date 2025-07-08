package zaya

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoogleAITokenCounter(t *testing.T) {
	counter := NewGoogleAITokenCounter()

	// Test text counting
	text := "Hello world"
	tokens := counter.CountText(text)
	require.Equal(t, 11, tokens)

	// Test small image
	tokens = counter.CountImage(Image{Width: 300, Height: 300})
	require.Equal(t, 258, tokens)

	// Test large image (2x2 tiles)
	tokens = counter.CountImage(Image{Width: 1000, Height: 1000})
	require.Equal(t, 4*258, tokens)

	// Test small video
	tokens = counter.CountVideo(Video{Width: 300, Height: 300, Duration: 10})
	require.Equal(t, 10*98, tokens)

	// Test large video
	tokens = counter.CountVideo(Video{Width: 500, Height: 400, Duration: 5})
	require.Equal(t, 5*290, tokens)

	// Test audio
	tokens = counter.CountAudio(Audio{Duration: 15})
	require.Equal(t, 15*32, tokens)
}

func TestBasicTokenCounter(t *testing.T) {
	counter := NewBasicTokenCounter()

	// Test text counting
	text := "Hello world"
	tokens := counter.CountText(text)
	require.Equal(t, 11, tokens)

	// Test image - should be much different from Google AI
	tokens = counter.CountImage(Image{Width: 300, Height: 300})
	require.NotEqual(t, 258, tokens) // Should not be the same as Google AI

	// Test video - should be different from Google AI
	tokens = counter.CountVideo(Video{Width: 300, Height: 300, Duration: 10})
	require.NotEqual(t, 10*98, tokens) // Should not be the same as Google AI

	// Test audio - should be different from Google AI
	tokens = counter.CountAudio(Audio{Duration: 15})
	require.NotEqual(t, 15*32, tokens) // Should not be the same as Google AI
	require.Equal(t, 15, tokens)       // Basic counter uses 1 token per second

	// Test that it implements the interface
	var _ TokenCounter = counter
}

func TestTokenCounterSelection(t *testing.T) {
	// Test Google AI provider selection
	cfg := AiConfig{
		Provider: "googleai",
		ApiKey:   "test-key",
		Model:    "test-model",
		MaxTok:   1000,
	}

	ai, ok := NewAI(cfg)
	require.True(t, ok)
	require.NotNil(t, ai)

	// Test that Google AI counter is selected
	tokens := ai.tokCntr.CountImage(Image{Width: 300, Height: 300})
	require.Equal(t, 258, tokens) // Google AI specific value

	// Test OpenAI provider selection (should use basic counter)
	cfg.Provider = "openai"
	ai, ok = NewAI(cfg)
	require.True(t, ok)
	require.NotNil(t, ai)

	// Test that Basic counter is selected
	tokens = ai.tokCntr.CountImage(Image{Width: 300, Height: 300})
	require.NotEqual(t, 258, tokens) // Should not be Google AI specific value

	// Test Mistral provider selection (should use basic counter)
	cfg.Provider = "mistral"
	ai, ok = NewAI(cfg)
	require.True(t, ok)
	require.NotNil(t, ai)

	// Test that Basic counter is selected
	tokens = ai.tokCntr.CountImage(Image{Width: 300, Height: 300})
	require.NotEqual(t, 258, tokens) // Should not be Google AI specific value
}

func TestTokenCounterInterface(t *testing.T) {
	// Test that both implementations satisfy the interface
	var googleCounter TokenCounter = NewGoogleAITokenCounter()
	var basicCounter TokenCounter = NewBasicTokenCounter()

	require.NotNil(t, googleCounter)
	require.NotNil(t, basicCounter)

	// Test that they produce different results for the same input
	imageTokensGoogle := googleCounter.CountImage(Image{Width: 500, Height: 500})
	imageTokensBasic := basicCounter.CountImage(Image{Width: 500, Height: 500})
	require.NotEqual(t, imageTokensGoogle, imageTokensBasic)

	audioTokensGoogle := googleCounter.CountAudio(Audio{Duration: 10})
	audioTokensBasic := basicCounter.CountAudio(Audio{Duration: 10})
	require.NotEqual(t, audioTokensGoogle, audioTokensBasic)
}
