package zaya

// GoogleAITokenCounter implements token counting logic specific to Google AI models
type GoogleAITokenCounter struct{}

// NewGoogleAITokenCounter creates a new GoogleAI token counter
func NewGoogleAITokenCounter() *GoogleAITokenCounter {
	return &GoogleAITokenCounter{}
}

// CountText counts tokens in text content
func (g *GoogleAITokenCounter) CountText(text string) int {
	return len(text)
}

// CountImage counts tokens for image content based on dimensions
// Uses Google AI's token calculation logic
func (g *GoogleAITokenCounter) CountImage(img Image) int {
	captionLen := g.CountText(img.Caption)
	if img.Width <= 384 && img.Height <= 384 {
		return captionLen + 258
	}

	// Calculate number of 768x768 tiles needed
	tilesX := (img.Width + 767) / 768  // Ceiling division
	tilesY := (img.Height + 767) / 768 // Ceiling division
	totalTiles := tilesX * tilesY

	return captionLen + totalTiles*258
}

// CountVideo counts tokens for video content based on dimensions and duration
// Uses Google AI's token calculation logic
func (g *GoogleAITokenCounter) CountVideo(video Video) int {
	captionLen := g.CountText(video.Caption)
	if video.Width <= 384 && video.Height <= 384 {
		return captionLen + video.Duration*98
	}
	return captionLen + video.Duration*290
}

// CountAudio counts tokens for audio content based on duration
// Uses Google AI's token calculation logic (32 tokens per second)
func (g *GoogleAITokenCounter) CountAudio(audio Audio) int {
	captionLen := g.CountText(audio.Caption)
	return captionLen + audio.Duration*32
}
