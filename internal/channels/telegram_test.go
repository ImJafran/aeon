package channels

import (
	"testing"
)

func TestChunkMessage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected int // number of chunks
	}{
		{
			name:     "short message",
			text:     "hello world",
			maxLen:   4096,
			expected: 1,
		},
		{
			name:     "exactly at limit",
			text:     string(make([]byte, 4096)),
			maxLen:   4096,
			expected: 1,
		},
		{
			name:     "needs two chunks",
			text:     string(make([]byte, 5000)),
			maxLen:   4096,
			expected: 2,
		},
		{
			name:     "needs three chunks",
			text:     string(make([]byte, 10000)),
			maxLen:   4096,
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkMessage(tt.text, tt.maxLen)
			if len(chunks) != tt.expected {
				t.Errorf("got %d chunks, want %d", len(chunks), tt.expected)
			}
			// Verify all content is preserved
			total := 0
			for _, c := range chunks {
				total += len(c)
				if len(c) > tt.maxLen {
					t.Errorf("chunk exceeds max length: %d > %d", len(c), tt.maxLen)
				}
			}
			if total != len(tt.text) {
				t.Errorf("lost content: total %d, original %d", total, len(tt.text))
			}
		})
	}
}

func TestChunkMessageWithNewlines(t *testing.T) {
	// Create text with newlines that should be used as split points
	text := ""
	for i := 0; i < 100; i++ {
		text += "This is a line of text that is repeated.\n"
	}

	chunks := chunkMessage(text, 200)
	for _, c := range chunks {
		if len(c) > 200 {
			t.Errorf("chunk exceeds max length: %d", len(c))
		}
	}

	// Verify content preserved
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != len(text) {
		t.Errorf("content not preserved: got %d, want %d", total, len(text))
	}
}
