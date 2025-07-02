package zaya

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// ContentExtractor handles web page content extraction with various strategies
type ContentExtractor struct {
	minContentLength int
	maxContentLength int
	removeElements   []string
	contentSelectors []string
}

// NewContentExtractor creates a new content extractor with default settings
func NewContentExtractor() *ContentExtractor {
	return &ContentExtractor{
		minContentLength: 100,
		maxContentLength: 100000,
		removeElements: []string{
			"script", "style", "nav", "header", "footer", "aside",
			"advertisement", "ads", "sidebar", "menu", "social",
			".ad", ".ads", ".advertisement", ".sidebar", ".menu",
			"#nav", "#header", "#footer", "#sidebar", "#menu",
		},
		contentSelectors: []string{
			"article",
			"main",
			"[role='main']",
			".content",
			".main-content",
			".post-content",
			".article-content",
			".entry-content",
			".text-content",
			"#content",
			"#main-content",
		},
	}
}

// GetPageText extracts the main text content from a web page URL
func GetPageText(url string) (string, error) {
	extractor := NewContentExtractor()
	return extractor.ExtractContent(url)
}

// ExtractContent fetches and extracts the main content from a web page
func (ce *ContentExtractor) ExtractContent(url string) (string, error) {
	// Fetch the page
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d %s", resp.StatusCode, resp.Status)
	}

	// Handle encoding detection and conversion
	body, err := ce.handleEncoding(resp)
	if err != nil {
		return "", fmt.Errorf("failed to handle page encoding: %w", err)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract content using multiple strategies
	content := ce.extractMainContent(doc)
	if content == "" {
		return "", fmt.Errorf("no meaningful content found on the page")
	}

	// Clean and validate content
	cleanedContent := ce.cleanText(content)
	if len(cleanedContent) < ce.minContentLength {
		return "", fmt.Errorf("extracted content too short (got %d chars, minimum %d)",
			len(cleanedContent), ce.minContentLength)
	}

	if len(cleanedContent) > ce.maxContentLength {
		cleanedContent = cleanedContent[:ce.maxContentLength] + "..."
	}

	return cleanedContent, nil
}

// handleEncoding detects and converts page encoding to UTF-8
func (ce *ContentExtractor) handleEncoding(resp *http.Response) (string, error) {
	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Try to detect encoding from Content-Type header
	contentType := resp.Header.Get("Content-Type")

	// Use charset detection
	reader, err := charset.NewReader(bytes.NewReader(bodyBytes), contentType)
	if err != nil {
		// Fallback: try to detect encoding from content
		reader = bytes.NewReader(bodyBytes)
	}

	// Convert to UTF-8
	utf8Reader := transform.NewReader(reader, unicode.UTF8.NewEncoder())
	utf8Bytes, err := io.ReadAll(utf8Reader)
	if err != nil {
		// If conversion fails, try the original content
		if utf8.Valid(bodyBytes) {
			return string(bodyBytes), nil
		}
		// Last resort: replace invalid UTF-8 sequences
		return string(bytes.ToValidUTF8(bodyBytes, []byte("�"))), nil
	}

	return string(utf8Bytes), nil
}

// extractMainContent uses multiple strategies to find the main content
func (ce *ContentExtractor) extractMainContent(doc *goquery.Document) string {
	// Remove unwanted elements first
	ce.removeUnwantedElements(doc)

	// Strategy 1: Try semantic HTML5 elements and common content selectors
	for _, selector := range ce.contentSelectors {
		content := ce.trySelector(doc, selector)
		if content != "" {
			return content
		}
	}

	// Strategy 2: Score content blocks by text density
	content := ce.scoreContentBlocks(doc)
	if content != "" {
		return content
	}

	// Strategy 3: Fallback to body content with filtering
	return ce.extractBodyContent(doc)
}

// removeUnwantedElements removes navigation, ads, scripts, etc.
func (ce *ContentExtractor) removeUnwantedElements(doc *goquery.Document) {
	for _, selector := range ce.removeElements {
		doc.Find(selector).Remove()
	}
}

// trySelector attempts to extract content using a specific CSS selector
func (ce *ContentExtractor) trySelector(doc *goquery.Document, selector string) string {
	var content strings.Builder
	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) > ce.minContentLength {
			if content.Len() > 0 {
				content.WriteString("\n\n")
			}
			content.WriteString(text)
		}
	})
	return content.String()
}

// scoreContentBlocks scores div elements by their text density and content quality
func (ce *ContentExtractor) scoreContentBlocks(doc *goquery.Document) string {
	bestScore := 0.0
	var bestContent string

	doc.Find("div, section, article").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) < ce.minContentLength {
			return
		}

		// Calculate content score based on various factors
		score := ce.calculateContentScore(text, s)

		if score > bestScore {
			bestScore = score
			bestContent = text
		}
	})

	return bestContent
}

// calculateContentScore calculates a score for content quality
func (ce *ContentExtractor) calculateContentScore(text string, selection *goquery.Selection) float64 {
	score := 0.0

	// Base score from text length
	score += float64(len(text)) / 100.0

	// Bonus for paragraph count
	paragraphs := len(strings.Split(text, "\n"))
	score += float64(paragraphs) * 2.0

	// Penalty for too many links
	linkCount := selection.Find("a").Length()
	textLength := len(text)
	if textLength > 0 {
		linkRatio := float64(linkCount) / float64(textLength) * 1000
		if linkRatio > 50 {
			score -= linkRatio
		}
	}

	// Bonus for semantic elements
	if selection.Is("article") {
		score += 50
	} else if selection.Is("main") {
		score += 30
	} else if selection.HasClass("content") || selection.HasClass("post") {
		score += 20
	}

	// Penalty for navigation-like classes
	class := selection.AttrOr("class", "")
	id := selection.AttrOr("id", "")
	combined := strings.ToLower(class + " " + id)

	penaltyKeywords := []string{"nav", "menu", "sidebar", "ad", "comment", "footer", "header"}
	for _, keyword := range penaltyKeywords {
		if strings.Contains(combined, keyword) {
			score -= 25
		}
	}

	return score
}

// extractBodyContent extracts content from body as last resort
func (ce *ContentExtractor) extractBodyContent(doc *goquery.Document) string {
	bodyText := doc.Find("body").Text()
	return strings.TrimSpace(bodyText)
}

// cleanText cleans and normalizes extracted text
func (ce *ContentExtractor) cleanText(text string) string {
	// Remove extra whitespace and normalize line breaks
	re := regexp.MustCompile(`\s+`)
	cleaned := re.ReplaceAllString(text, " ")

	// Remove leading/trailing whitespace
	cleaned = strings.TrimSpace(cleaned)

	// Normalize quotes and other characters
	cleaned = strings.ReplaceAll(cleaned, "\u201c", "\"") // Left double quotation mark
	cleaned = strings.ReplaceAll(cleaned, "\u201d", "\"") // Right double quotation mark
	cleaned = strings.ReplaceAll(cleaned, "\u2018", "'")  // Left single quotation mark
	cleaned = strings.ReplaceAll(cleaned, "\u2019", "'")  // Right single quotation mark
	cleaned = strings.ReplaceAll(cleaned, "\u2013", "-")  // En dash
	cleaned = strings.ReplaceAll(cleaned, "\u2014", "-")  // Em dash

	return cleaned
}

// SetMinContentLength sets the minimum content length requirement
func (ce *ContentExtractor) SetMinContentLength(length int) {
	ce.minContentLength = length
}

// SetMaxContentLength sets the maximum content length limit
func (ce *ContentExtractor) SetMaxContentLength(length int) {
	ce.maxContentLength = length
}

// AddContentSelector adds a CSS selector to try for content extraction
func (ce *ContentExtractor) AddContentSelector(selector string) {
	ce.contentSelectors = append(ce.contentSelectors, selector)
}

// AddRemoveElement adds an element/selector to remove during extraction
func (ce *ContentExtractor) AddRemoveElement(selector string) {
	ce.removeElements = append(ce.removeElements, selector)
}

// UTF16OffsetToUTF8 converts UTF-16 offset and length to UTF-8 byte positions
// This is needed because Telegram uses UTF-16 code units for entity offsets
// while Go strings are UTF-8 encoded
func UTF16OffsetToUTF8(text string, utf16Offset, utf16Length int) (int, int) {
	// Convert string to runes for proper Unicode handling
	runes := []rune(text)

	// Count UTF-16 code units to find the start position
	utf16Count := 0
	utf8Start := 0

	for i, r := range runes {
		if utf16Count >= utf16Offset {
			utf8Start = len(string(runes[:i]))
			break
		}
		// Check if rune requires surrogate pair in UTF-16
		if r > 0xFFFF {
			utf16Count += 2 // Surrogate pair
		} else {
			utf16Count += 1
		}
	}

	// Find the end position
	utf16End := utf16Offset + utf16Length
	utf8End := len(text) // Default to end of string

	utf16Count = 0
	for i, r := range runes {
		if utf16Count >= utf16End {
			utf8End = len(string(runes[:i]))
			break
		}
		// Check if rune requires surrogate pair in UTF-16
		if r > 0xFFFF {
			utf16Count += 2 // Surrogate pair
		} else {
			utf16Count += 1
		}
	}

	return utf8Start, utf8End
}
