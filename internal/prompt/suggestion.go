package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Prompt suggestion service — aligned with claude-code-main PromptSuggestion/.
//
// Generates contextual prompt suggestions based on the current session state,
// recent tool use, errors, and user patterns. Suggestions help guide users
// toward productive next actions.

// SuggestionType categorizes the kind of suggestion.
type SuggestionType string

const (
	SuggestionTypeAction   SuggestionType = "action"   // actionable next step
	SuggestionTypeFollowUp SuggestionType = "followup" // follow-up question
	SuggestionTypeRepair   SuggestionType = "repair"   // fix a recent error
	SuggestionTypeExplore  SuggestionType = "explore"  // explore related topics
)

// Suggestion is a single prompt suggestion.
type Suggestion struct {
	Text        string         `json:"text"`
	Type        SuggestionType `json:"type"`
	Description string         `json:"description,omitempty"`
	Priority    int            `json:"priority"` // higher = more relevant
	Source      string         `json:"source"`   // "builtin", "context", "ai"
}

// SuggestionContext captures the state used to generate suggestions.
type SuggestionContext struct {
	// LastToolName is the most recently used tool (empty if none).
	LastToolName string
	// LastToolError is set if the last tool call failed.
	LastToolError string
	// LastAssistantText is the tail of the last assistant message.
	LastAssistantText string
	// WorkDir is the current working directory.
	WorkDir string
	// TurnCount is the number of conversation turns so far.
	TurnCount int
	// HasGitRepo indicates whether the working directory is a git repo.
	HasGitRepo bool
	// FilesMentioned is a list of recently referenced file paths.
	FilesMentioned []string
}

// SuggestionEvaluator generates AI-powered suggestions given a context summary.
type SuggestionEvaluator func(ctx context.Context, prompt string) (string, error)

// SuggestionService generates prompt suggestions.
type SuggestionService struct {
	mu        sync.RWMutex
	builtins  []suggestionRule
	evaluator SuggestionEvaluator
	cache     []Suggestion
	cacheTime time.Time
	cacheTTL  time.Duration
}

// NewSuggestionService creates a new suggestion service with builtin rules.
func NewSuggestionService() *SuggestionService {
	svc := &SuggestionService{
		cacheTTL: 30 * time.Second,
	}
	svc.builtins = defaultSuggestionRules()
	return svc
}

// SetEvaluator sets the AI evaluator for context-aware suggestions.
func (s *SuggestionService) SetEvaluator(fn SuggestionEvaluator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluator = fn
}

// Suggest generates suggestions for the given context.
// It combines builtin rule-based suggestions with optional AI suggestions.
func (s *SuggestionService) Suggest(ctx context.Context, sctx SuggestionContext, maxResults int) []Suggestion {
	if maxResults <= 0 {
		maxResults = 5
	}

	// Check cache.
	s.mu.RLock()
	if time.Since(s.cacheTime) < s.cacheTTL && len(s.cache) > 0 {
		cached := s.cache
		s.mu.RUnlock()
		if len(cached) > maxResults {
			return cached[:maxResults]
		}
		return cached
	}
	s.mu.RUnlock()

	var suggestions []Suggestion

	// Apply builtin rules.
	for _, rule := range s.builtins {
		if sg := rule(sctx); sg != nil {
			suggestions = append(suggestions, *sg)
		}
	}

	// Apply AI evaluator if available.
	s.mu.RLock()
	evaluator := s.evaluator
	s.mu.RUnlock()

	if evaluator != nil {
		aiSuggestions := s.getAISuggestions(ctx, evaluator, sctx)
		suggestions = append(suggestions, aiSuggestions...)
	}

	// Sort by priority descending.
	sortSuggestions(suggestions)

	if len(suggestions) > maxResults {
		suggestions = suggestions[:maxResults]
	}

	// Cache results.
	s.mu.Lock()
	s.cache = suggestions
	s.cacheTime = time.Now()
	s.mu.Unlock()

	return suggestions
}

// getAISuggestions asks the LLM for contextual suggestions.
func (s *SuggestionService) getAISuggestions(ctx context.Context, evaluator SuggestionEvaluator, sctx SuggestionContext) []Suggestion {
	prompt := buildSuggestionPrompt(sctx)

	response, err := evaluator(ctx, prompt)
	if err != nil {
		slog.Debug("suggestion AI evaluation failed", "error", err)
		return nil
	}

	return parseSuggestionResponse(response)
}

// ────────────────────────────────────────────────────────────────────────────
// Builtin suggestion rules
// ────────────────────────────────────────────────────────────────────────────

type suggestionRule func(SuggestionContext) *Suggestion

func defaultSuggestionRules() []suggestionRule {
	return []suggestionRule{
		ruleErrorRecovery,
		rulePostEdit,
		rulePostTest,
		ruleFirstTurn,
		ruleGitRepo,
	}
}

func ruleErrorRecovery(ctx SuggestionContext) *Suggestion {
	if ctx.LastToolError == "" {
		return nil
	}
	return &Suggestion{
		Text:        "Can you fix the error in the last step?",
		Type:        SuggestionTypeRepair,
		Description: "The last tool call returned an error",
		Priority:    100,
		Source:      "builtin",
	}
}

func rulePostEdit(ctx SuggestionContext) *Suggestion {
	if ctx.LastToolName != "Edit" && ctx.LastToolName != "Write" && ctx.LastToolName != "MultiEdit" {
		return nil
	}
	return &Suggestion{
		Text:        "Run tests to verify the changes",
		Type:        SuggestionTypeAction,
		Description: "After editing files, verify with tests",
		Priority:    80,
		Source:      "builtin",
	}
}

func rulePostTest(ctx SuggestionContext) *Suggestion {
	if ctx.LastToolName != "Bash" {
		return nil
	}
	if ctx.LastToolError != "" {
		return &Suggestion{
			Text:        "Fix the failing test and try again",
			Type:        SuggestionTypeRepair,
			Description: "A test or command failed",
			Priority:    90,
			Source:      "builtin",
		}
	}
	return nil
}

func ruleFirstTurn(ctx SuggestionContext) *Suggestion {
	if ctx.TurnCount > 1 {
		return nil
	}
	return &Suggestion{
		Text:        "What would you like to work on?",
		Type:        SuggestionTypeExplore,
		Description: "Start of session",
		Priority:    50,
		Source:      "builtin",
	}
}

func ruleGitRepo(ctx SuggestionContext) *Suggestion {
	if !ctx.HasGitRepo || ctx.TurnCount > 3 {
		return nil
	}
	return &Suggestion{
		Text:        "Show me the recent git changes",
		Type:        SuggestionTypeExplore,
		Description: "Review recent changes in the repo",
		Priority:    40,
		Source:      "builtin",
	}
}

// ────────────────────────────────────────────────────────────────────────────
// AI suggestion prompt & parsing
// ────────────────────────────────────────────────────────────────────────────

func buildSuggestionPrompt(sctx SuggestionContext) string {
	var sb strings.Builder
	sb.WriteString("Generate 3 contextual prompt suggestions for the user based on this session state:\n\n")
	sb.WriteString(fmt.Sprintf("- Turn count: %d\n", sctx.TurnCount))
	sb.WriteString(fmt.Sprintf("- Working directory: %s\n", sctx.WorkDir))
	sb.WriteString(fmt.Sprintf("- Last tool: %s\n", sctx.LastToolName))
	if sctx.LastToolError != "" {
		sb.WriteString(fmt.Sprintf("- Last error: %.200s\n", sctx.LastToolError))
	}
	if sctx.LastAssistantText != "" {
		tail := sctx.LastAssistantText
		if len(tail) > 300 {
			tail = tail[len(tail)-300:]
		}
		sb.WriteString(fmt.Sprintf("- Last assistant text (tail): %.300s\n", tail))
	}
	sb.WriteString(fmt.Sprintf("- Git repo: %v\n", sctx.HasGitRepo))
	if len(sctx.FilesMentioned) > 0 {
		sb.WriteString(fmt.Sprintf("- Recent files: %s\n", strings.Join(sctx.FilesMentioned, ", ")))
	}

	sb.WriteString(`
Return a JSON array of suggestions. Each item:
{"text": "...", "type": "action|followup|repair|explore", "description": "...", "priority": 0-100}

Respond ONLY with valid JSON array, no markdown fencing.`)
	return sb.String()
}

func parseSuggestionResponse(response string) []Suggestion {
	response = strings.TrimSpace(response)
	// Strip markdown fencing.
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			response = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var raw []struct {
		Text        string `json:"text"`
		Type        string `json:"type"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
	}
	if err := json.Unmarshal([]byte(response), &raw); err != nil {
		return nil
	}

	var suggestions []Suggestion
	for _, r := range raw {
		suggestions = append(suggestions, Suggestion{
			Text:        r.Text,
			Type:        SuggestionType(r.Type),
			Description: r.Description,
			Priority:    r.Priority,
			Source:      "ai",
		})
	}
	return suggestions
}

func sortSuggestions(suggestions []Suggestion) {
	for i := 1; i < len(suggestions); i++ {
		for j := i; j > 0 && suggestions[j].Priority > suggestions[j-1].Priority; j-- {
			suggestions[j], suggestions[j-1] = suggestions[j-1], suggestions[j]
		}
	}
}
