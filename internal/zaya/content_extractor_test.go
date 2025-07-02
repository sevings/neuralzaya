package zaya

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentExtractor_ExtractContent(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		expectedLength int
		shouldContain  string
		shouldError    bool
	}{
		{
			name: "article tag content",
			html: `
				<html>
					<head><title>Test</title></head>
					<body>
						<nav>Navigation menu</nav>
						<article>
							<h1>Main Article Title</h1>
							<p>This is the main content of the article that should be extracted. It contains meaningful information that users want to read.</p>
							<p>Another paragraph with more useful content.</p>
						</article>
						<footer>Footer content</footer>
					</body>
				</html>
			`,
			expectedLength: 100,
			shouldContain:  "Main Article Title",
			shouldError:    false,
		},
		{
			name: "main tag content",
			html: `
				<html>
					<body>
						<header>Header</header>
						<main>
							<h1>Main Content</h1>
							<p>This is the main content section with important information that should be extracted for processing.</p>
						</main>
						<aside>Sidebar content</aside>
					</body>
				</html>
			`,
			expectedLength: 50,
			shouldContain:  "Main Content",
			shouldError:    false,
		},
		{
			name: "content scoring fallback",
			html: `
				<html>
					<body>
						<div class="header">Header with navigation</div>
						<div class="content">
							<h2>Important Content</h2>
							<p>This div has a content class and should be prioritized over other divs when extracting meaningful content.</p>
							<p>Multiple paragraphs increase the content score.</p>
							<p>This makes it more likely to be selected as the main content.</p>
						</div>
						<div class="sidebar">Sidebar with ads</div>
					</body>
				</html>
			`,
			expectedLength: 100,
			shouldContain:  "Important Content",
			shouldError:    false,
		},
		{
			name: "remove unwanted elements",
			html: `
				<html>
					<body>
						<div>
							<script>alert('remove me');</script>
							<style>.test { color: red; }</style>
							<p>This is the actual content that should remain after removing scripts and styles. It contains enough text to meet the minimum length requirement for extraction and processing.</p>
							<nav>Navigation to remove</nav>
						</div>
					</body>
				</html>
			`,
			expectedLength: 100,
			shouldContain:  "actual content",
			shouldError:    false,
		},
		{
			name: "too short content",
			html: `
				<html>
					<body>
						<p>Short</p>
					</body>
				</html>
			`,
			expectedLength: 0,
			shouldContain:  "",
			shouldError:    true,
		},
		{
			name: "unicode content",
			html: `
				<html>
					<body>
						<article>
							<h1>Тест Unicode</h1>
							<p>Этот текст содержит unicode символы и должен быть правильно обработан. Проверяем работу с различными кодировками и символами.</p>
							<p>测试中文字符处理能力。这些字符应该被正确提取和处理。</p>
							<p>Testing émojis and spëcial characters: 🚀 ñoël café résumé</p>
						</article>
					</body>
				</html>
			`,
			expectedLength: 100,
			shouldContain:  "unicode",
			shouldError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.html))
			}))
			defer server.Close()

			// Test extraction
			extractor := NewContentExtractor()
			content, err := extractor.ExtractContent(server.URL)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(content) < tt.expectedLength {
				t.Errorf("Content too short: got %d chars, expected at least %d", len(content), tt.expectedLength)
			}

			if tt.shouldContain != "" && !strings.Contains(strings.ToLower(content), strings.ToLower(tt.shouldContain)) {
				t.Errorf("Content doesn't contain expected text '%s'. Got: %s", tt.shouldContain, content)
			}
		})
	}
}

func TestContentExtractor_CleanText(t *testing.T) {
	extractor := NewContentExtractor()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normalize whitespace",
			input:    "Text   with    multiple     spaces\n\n\nand\tlines",
			expected: "Text with multiple spaces and lines",
		},
		{
			name:     "smart quotes",
			input:    "\u201cHello world\u201d and \u2018test\u2019 with \u2013 and \u2014",
			expected: "\"Hello world\" and 'test' with - and -",
		},
		{
			name:     "trim whitespace",
			input:    "   \n\t  Text with surrounding whitespace  \n\t   ",
			expected: "Text with surrounding whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.cleanText(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestContentExtractor_CalculateContentScore(t *testing.T) {
	// This test uses a mock HTML structure
	html := `
		<div class="content">
			<p>This is a paragraph with good content.</p>
			<p>Another paragraph that adds to the score.</p>
			<p>Third paragraph that adds to the score.</p>
			<a href="#">Link</a>
		</div>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>" + html + "</body></html>"))
	}))
	defer server.Close()

	// We can't easily test the scoring function directly since it requires goquery selection,
	// but we can test that content with good indicators gets selected
	extractor := NewContentExtractor()
	content, err := extractor.ExtractContent(server.URL)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !strings.Contains(content, "good content") {
		t.Errorf("Expected content to contain 'good content', got: %s", content)
	}
}

func TestUTF16OffsetToUTF8(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		utf16Offset  int
		utf16Length  int
		expectedText string
	}{
		{
			name:         "simple ASCII",
			text:         "Hello world!",
			utf16Offset:  6,
			utf16Length:  5,
			expectedText: "world",
		},
		{
			name:         "with emoji",
			text:         "Hello 🌍 world!",
			utf16Offset:  6,
			utf16Length:  2, // Emoji takes 2 UTF-16 code units
			expectedText: "🌍",
		},
		{
			name:         "cyrillic text",
			text:         "Привет мир!",
			utf16Offset:  7,
			utf16Length:  3,
			expectedText: "мир",
		},
		{
			name:         "mixed unicode",
			text:         "URL: https://example.com 🔗",
			utf16Offset:  5,
			utf16Length:  19,
			expectedText: "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := UTF16OffsetToUTF8(tt.text, tt.utf16Offset, tt.utf16Length)
			result := tt.text[start:end]

			if result != tt.expectedText {
				t.Errorf("Expected '%s', got '%s' (start: %d, end: %d)", tt.expectedText, result, start, end)
			}
		})
	}
}

func TestGetPageText(t *testing.T) {
	// Test the main function
	html := `
		<html>
			<head><title>Test Page</title></head>
			<body>
				<article>
					<h1>Test Article</h1>
					<p>This is a test article with enough content to pass the minimum length requirement for extraction.</p>
				</article>
			</body>
		</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}))
	defer server.Close()

	content, err := GetPageText(server.URL)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !strings.Contains(content, "Test Article") {
		t.Errorf("Expected content to contain 'Test Article', got: %s", content)
	}

	if len(content) < 50 {
		t.Errorf("Content too short: %d chars", len(content))
	}
}

func TestContentExtractor_HandleBadEncoding(t *testing.T) {
	// Test with invalid UTF-8 that should be handled gracefully
	badHTML := []byte{0xFF, 0xFE} // Invalid UTF-8 sequence
	badHTML = append(badHTML, []byte("<html><body><article>Content</article></body></html>")...)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(badHTML)
	}))
	defer server.Close()

	extractor := NewContentExtractor()
	// This should not panic and should handle the encoding gracefully
	_, err := extractor.ExtractContent(server.URL)

	// We expect either success (if encoding was fixed) or a specific error
	// The important thing is that it doesn't panic
	if err != nil {
		t.Logf("Encoding handling resulted in error (expected): %v", err)
	}
}
