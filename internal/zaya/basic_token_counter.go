package zaya

// BasicTokenCounter implements a simple token counting logic that uses data length
type BasicTokenCounter struct{}

// NewBasicTokenCounter creates a new basic token counter
func NewBasicTokenCounter() *BasicTokenCounter {
	return &BasicTokenCounter{}
}

// CountText counts tokens in text content
func (b *BasicTokenCounter) CountText(text string) int {
	return len(text)
}

// CountImage counts tokens for image content based on dimensions
func (b *BasicTokenCounter) CountImage(img Image) int {
	captionLen := b.CountText(img.Caption)
	// Simple approximation based on image dimensions
	// Assume average compression ratio and bytes per pixel
	estimatedBytes := img.Width * img.Height * 3 / 10 // rough JPEG compression estimate
	return captionLen + estimatedBytes/100
}

// CountVideo counts tokens for video content based on dimensions and duration
func (b *BasicTokenCounter) CountVideo(video Video) int {
	captionLen := b.CountText(video.Caption)

	// Simple approximation based on video properties
	// Estimate video size and convert to tokens
	estimatedBytes := video.Width * video.Height * video.Duration * 3 / 100 // rough video compression estimate
	return captionLen + estimatedBytes/1000
}

// CountAudio counts tokens for audio content based on duration
func (b *BasicTokenCounter) CountAudio(audio Audio) int {
	captionLen := b.CountText(audio.Caption)
	return captionLen + audio.Duration
}
