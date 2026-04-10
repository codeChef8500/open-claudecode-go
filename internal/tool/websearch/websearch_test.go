package websearch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDDGHTML(t *testing.T) {
	// Minimal mock of DuckDuckGo HTML search response.
	mockHTML := `<html><body>
		<div class="result results_links results_links_deep web-result">
			<h2 class="result__title">
				<a class="result__a" href="https://example.com/page1">Example Page One</a>
			</h2>
			<a class="result__snippet" href="https://example.com/page1">This is the first snippet.</a>
		</div>
		<div class="result results_links results_links_deep web-result">
			<h2 class="result__title">
				<a class="result__a" href="https://example.org/page2">Example Page Two</a>
			</h2>
			<a class="result__snippet" href="https://example.org/page2">This is the second snippet.</a>
		</div>
		<div class="result results_links results_links_deep web-result">
			<h2 class="result__title">
				<a class="result__a" href="https://other.com/page3">Other Page Three</a>
			</h2>
			<a class="result__snippet" href="https://other.com/page3">Third snippet here.</a>
		</div>
	</body></html>`

	t.Run("basic parsing", func(t *testing.T) {
		hits, err := parseDDGHTML([]byte(mockHTML), 10, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 3 {
			t.Fatalf("expected 3 hits, got %d", len(hits))
		}
		if hits[0].Title != "Example Page One" {
			t.Errorf("expected title 'Example Page One', got %q", hits[0].Title)
		}
		if hits[0].URL != "https://example.com/page1" {
			t.Errorf("expected URL 'https://example.com/page1', got %q", hits[0].URL)
		}
		if hits[0].Snippet != "This is the first snippet." {
			t.Errorf("expected snippet 'This is the first snippet.', got %q", hits[0].Snippet)
		}
	})

	t.Run("max results limit", func(t *testing.T) {
		hits, err := parseDDGHTML([]byte(mockHTML), 2, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 2 {
			t.Fatalf("expected 2 hits, got %d", len(hits))
		}
	})

	t.Run("allowed domains", func(t *testing.T) {
		hits, err := parseDDGHTML([]byte(mockHTML), 10, []string{"example.com"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 1 {
			t.Fatalf("expected 1 hit for allowed domain, got %d", len(hits))
		}
		if hits[0].URL != "https://example.com/page1" {
			t.Errorf("expected URL from example.com, got %q", hits[0].URL)
		}
	})

	t.Run("blocked domains", func(t *testing.T) {
		hits, err := parseDDGHTML([]byte(mockHTML), 10, nil, []string{"other.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 2 {
			t.Fatalf("expected 2 hits with other.com blocked, got %d", len(hits))
		}
	})

	t.Run("redirect URL extraction", func(t *testing.T) {
		redirectHTML := `<html><body>
			<div class="result">
				<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Freal.example.com%2Fpage">Redirect Test</a>
				<a class="result__snippet" href="#">A snippet.</a>
			</div>
		</body></html>`
		hits, err := parseDDGHTML([]byte(redirectHTML), 10, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 1 {
			t.Fatalf("expected 1 hit, got %d", len(hits))
		}
		if hits[0].URL != "https://real.example.com/page" {
			t.Errorf("expected redirected URL, got %q", hits[0].URL)
		}
	})

	t.Run("empty HTML", func(t *testing.T) {
		hits, err := parseDDGHTML([]byte("<html><body></body></html>"), 10, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("expected 0 hits, got %d", len(hits))
		}
	})
}

func TestValidateInput(t *testing.T) {
	tool := New("", "")

	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr string
	}{
		{
			name:    "empty query",
			input:   map[string]interface{}{"query": ""},
			wantErr: "query must not be empty",
		},
		{
			name:    "negative max_results",
			input:   map[string]interface{}{"query": "test", "max_results": -1},
			wantErr: "max_results must be non-negative",
		},
		{
			name:    "max_results too high",
			input:   map[string]interface{}{"query": "test", "max_results": 100},
			wantErr: "max_results exceeds maximum",
		},
		{
			name:    "valid query",
			input:   map[string]interface{}{"query": "golang concurrency"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.input)
			err := tool.ValidateInput(context.Background(), raw)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}
