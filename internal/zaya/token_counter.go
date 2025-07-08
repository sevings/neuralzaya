package zaya

// TokenCounter provides methods for counting tokens in different types of content
type TokenCounter interface {
	// CountText counts tokens in text content
	CountText(text string) int

	// CountImage counts tokens for image content based on dimensions
	CountImage(img Image) int

	// CountVideo counts tokens for video content based on dimensions and duration
	CountVideo(video Video) int

	// CountAudio counts tokens for audio content based on duration
	CountAudio(audio Audio) int
}
