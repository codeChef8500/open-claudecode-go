package constants

// System prompt boundary marker ported from constants/prompts.ts:L114-115.
// [P1.T2] TS anchor: constants/prompts.ts:L114-115
//
// Everything BEFORE this marker in the system prompt array can use
// scope: 'global' for caching. Everything AFTER contains user/session-
// specific content and should NOT be cached.
//
// WARNING: Do not remove or reorder this marker without updating cache logic.
const SystemPromptDynamicBoundary = "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__"

// ClaudeCodeDocsMapURL is the documentation map URL referenced in prompts.
// TS anchor: constants/prompts.ts:L102-103
const ClaudeCodeDocsMapURL = "https://code.claude.com/docs/en/claude_code_docs_map.md"
