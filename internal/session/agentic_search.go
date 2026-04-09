package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Agentic session search — aligned with claude-code-main agenticSessionSearch.ts.
//
// Uses an LLM to semantically search across session history, going beyond
// simple keyword matching. The LLM evaluates session summaries against the
// user's query and ranks them by relevance.

// AgenticSearcher performs AI-powered session search.
type AgenticSearcher struct {
	storage   *Storage
	evaluator AgenticEvaluator
}

// AgenticEvaluator sends a prompt to an LLM and returns the response text.
type AgenticEvaluator func(ctx context.Context, prompt string) (string, error)

// AgenticSearchResult is a session that matched the semantic search.
type AgenticSearchResult struct {
	SessionID   string  `json:"session_id"`
	Title       string  `json:"title"`
	WorkDir     string  `json:"work_dir"`
	CreatedAt   time.Time `json:"created_at"`
	Relevance   float64 `json:"relevance"`
	Explanation string  `json:"explanation"`
}

// NewAgenticSearcher creates a new agentic session searcher.
func NewAgenticSearcher(storage *Storage, evaluator AgenticEvaluator) *AgenticSearcher {
	return &AgenticSearcher{
		storage:   storage,
		evaluator: evaluator,
	}
}

// Search performs semantic search across sessions using the LLM.
// It first gathers session summaries, then asks the LLM to rank them
// by relevance to the query.
func (a *AgenticSearcher) Search(ctx context.Context, query string, maxResults int) ([]AgenticSearchResult, error) {
	if a.evaluator == nil {
		// Fall back to keyword search if no evaluator.
		return a.fallbackKeywordSearch(query, maxResults)
	}

	if maxResults <= 0 {
		maxResults = 10
	}

	// Gather recent sessions with summaries.
	metas, err := a.storage.Recent(50)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	if len(metas) == 0 {
		return nil, nil
	}

	// Build session summaries for the LLM.
	var summaries []sessionSummaryEntry
	for _, m := range metas {
		entry := sessionSummaryEntry{
			ID:        m.ID,
			Title:     m.Summary,
			WorkDir:   m.WorkDir,
			CreatedAt: m.CreatedAt.Format(time.RFC3339),
			TurnCount: m.TurnCount,
		}
		summaries = append(summaries, entry)
	}

	// Send to LLM for ranking.
	prompt := buildAgenticSearchPrompt(query, summaries, maxResults)

	response, err := a.evaluator(ctx, prompt)
	if err != nil {
		slog.Warn("agentic search LLM call failed, falling back to keyword", "error", err)
		return a.fallbackKeywordSearch(query, maxResults)
	}

	// Parse LLM response.
	results, err := parseAgenticSearchResponse(response, metas)
	if err != nil {
		slog.Warn("agentic search parse failed, falling back to keyword", "error", err)
		return a.fallbackKeywordSearch(query, maxResults)
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// fallbackKeywordSearch performs a basic keyword search when LLM is unavailable.
func (a *AgenticSearcher) fallbackKeywordSearch(query string, maxResults int) ([]AgenticSearchResult, error) {
	metas, err := a.storage.Search(query)
	if err != nil {
		return nil, err
	}

	if maxResults > 0 && len(metas) > maxResults {
		metas = metas[:maxResults]
	}

	var results []AgenticSearchResult
	for _, m := range metas {
		results = append(results, AgenticSearchResult{
			SessionID:   m.ID,
			Title:       m.Summary,
			WorkDir:     m.WorkDir,
			CreatedAt:   m.CreatedAt,
			Relevance:   0.5,
			Explanation: "keyword match",
		})
	}
	return results, nil
}

type sessionSummaryEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	WorkDir   string `json:"work_dir"`
	CreatedAt string `json:"created_at"`
	TurnCount int    `json:"turn_count"`
}

type agenticSearchRanking struct {
	Results []agenticRankedSession `json:"results"`
}

type agenticRankedSession struct {
	ID          string  `json:"id"`
	Relevance   float64 `json:"relevance"`
	Explanation string  `json:"explanation"`
}

func buildAgenticSearchPrompt(query string, summaries []sessionSummaryEntry, maxResults int) string {
	summaryJSON, _ := json.MarshalIndent(summaries, "", "  ")

	return fmt.Sprintf(`You are a session search assistant. Given a user query and a list of session summaries, rank the sessions by relevance to the query.

User query: %q

Sessions:
%s

Return a JSON object with a "results" array. Each result has:
- "id": the session ID
- "relevance": a score from 0.0 to 1.0
- "explanation": a brief reason for the ranking

Only include sessions with relevance > 0.2. Return at most %d results, sorted by relevance descending.

Respond ONLY with valid JSON, no markdown fencing.`, query, string(summaryJSON), maxResults)
}

func parseAgenticSearchResponse(response string, metas []*SessionMetadata) ([]AgenticSearchResult, error) {
	response = strings.TrimSpace(response)

	// Strip markdown fencing if present.
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
		response = strings.TrimSpace(response)
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
		response = strings.TrimSpace(response)
	}

	var ranking agenticSearchRanking
	if err := json.Unmarshal([]byte(response), &ranking); err != nil {
		return nil, fmt.Errorf("parse agentic search response: %w", err)
	}

	// Build a lookup map for metadata.
	metaMap := make(map[string]*SessionMetadata, len(metas))
	for _, m := range metas {
		metaMap[m.ID] = m
	}

	var results []AgenticSearchResult
	for _, r := range ranking.Results {
		meta := metaMap[r.ID]
		if meta == nil {
			continue
		}
		results = append(results, AgenticSearchResult{
			SessionID:   r.ID,
			Title:       meta.Summary,
			WorkDir:     meta.WorkDir,
			CreatedAt:   meta.CreatedAt,
			Relevance:   r.Relevance,
			Explanation: r.Explanation,
		})
	}

	return results, nil
}
