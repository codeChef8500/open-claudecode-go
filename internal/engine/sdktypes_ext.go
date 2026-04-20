package engine

// ────────────────────────────────────────────────────────────────────────────
// [P7] Extended SDK message types — aligned with claude-code-main
// entrypoints/sdk/coreSchemas.ts SDKMessage union.
// ────────────────────────────────────────────────────────────────────────────

// Additional SDKMessageType discriminators beyond the base set in sdktypes.go.
const (
	SDKMsgStreamEvent            SDKMessageType = "stream_event"
	SDKMsgResult                 SDKMessageType = "result"
	SDKMsgRateLimitEvent         SDKMessageType = "rate_limit_event"
	SDKMsgStreamlinedText        SDKMessageType = "streamlined_text"
	SDKMsgStreamlinedToolSummary SDKMessageType = "streamlined_tool_use_summary"
	SDKMsgToolProgress           SDKMessageType = "tool_progress"
	SDKMsgAuthStatus             SDKMessageType = "auth_status"
	SDKMsgPromptSuggestion       SDKMessageType = "prompt_suggestion"
)

// SDKSystemSubtype enumerates the system message subtypes.
type SDKSystemSubtype string

const (
	SDKSystemSubtypeInit                SDKSystemSubtype = "init"
	SDKSystemSubtypeCompactBoundary     SDKSystemSubtype = "compact_boundary"
	SDKSystemSubtypeStatus              SDKSystemSubtype = "status"
	SDKSystemSubtypeAPIRetry            SDKSystemSubtype = "api_retry"
	SDKSystemSubtypeLocalCommandOutput  SDKSystemSubtype = "local_command_output"
	SDKSystemSubtypeHookStarted         SDKSystemSubtype = "hook_started"
	SDKSystemSubtypeHookProgress        SDKSystemSubtype = "hook_progress"
	SDKSystemSubtypeHookResponse        SDKSystemSubtype = "hook_response"
	SDKSystemSubtypeTaskNotification    SDKSystemSubtype = "task_notification"
	SDKSystemSubtypeTaskStarted         SDKSystemSubtype = "task_started"
	SDKSystemSubtypeTaskProgress        SDKSystemSubtype = "task_progress"
	SDKSystemSubtypeSessionStateChanged SDKSystemSubtype = "session_state_changed"
	SDKSystemSubtypeFilesPersisted      SDKSystemSubtype = "files_persisted"
	SDKSystemSubtypePostTurnSummary     SDKSystemSubtype = "post_turn_summary"
	SDKSystemSubtypeElicitationComplete SDKSystemSubtype = "elicitation_complete"
)

// SDKAssistantMessageErrorType enumerates API error classes.
type SDKAssistantMessageErrorType string

const (
	SDKErrAuthFailed     SDKAssistantMessageErrorType = "authentication_failed"
	SDKErrBilling        SDKAssistantMessageErrorType = "billing_error"
	SDKErrRateLimit      SDKAssistantMessageErrorType = "rate_limit"
	SDKErrInvalidRequest SDKAssistantMessageErrorType = "invalid_request"
	SDKErrServer         SDKAssistantMessageErrorType = "server_error"
	SDKErrUnknown        SDKAssistantMessageErrorType = "unknown"
	SDKErrMaxOutput      SDKAssistantMessageErrorType = "max_output_tokens"
)

// SDKResultSubtype enumerates result outcomes.
type SDKResultSubtype string

const (
	SDKResultSuccess                   SDKResultSubtype = "success"
	SDKResultErrorDuringExecution      SDKResultSubtype = "error_during_execution"
	SDKResultErrorMaxTurns             SDKResultSubtype = "error_max_turns"
	SDKResultErrorMaxBudgetUSD         SDKResultSubtype = "error_max_budget_usd"
	SDKResultErrorMaxStructuredRetries SDKResultSubtype = "error_max_structured_output_retries"
)

// SDKSessionState enumerates session states.
type SDKSessionState string

const (
	SDKSessionIdle           SDKSessionState = "idle"
	SDKSessionRunning        SDKSessionState = "running"
	SDKSessionRequiresAction SDKSessionState = "requires_action"
)

// FastModeState tracks the fast mode toggle state.
type FastModeState string

const (
	FastModeOff      FastModeState = "off"
	FastModeCooldown FastModeState = "cooldown"
	FastModeOn       FastModeState = "on"
)

// ── Assistant turn message ──────────────────────────────────────────────────
// [P7.T1] TS anchor: coreSchemas.ts:SDKAssistantMessageSchema (L1347-1356)
// Named SDKAssistantTurnMessage to avoid collision with legacy SDKAssistantMessage
// in sdktypes.go (removed in P8.T6).

// SDKAssistantTurnMessage is the TS-aligned top-level assistant message.
type SDKAssistantTurnMessage struct {
	Type            SDKMessageType               `json:"type"`    // "assistant"
	Message         interface{}                  `json:"message"` // Anthropic API Message
	ParentToolUseID *string                      `json:"parent_tool_use_id"`
	Error           SDKAssistantMessageErrorType `json:"error,omitempty"`
	UUID            string                       `json:"uuid"`
	SessionID       string                       `json:"session_id"`
}

// ── User turn message ──────────────────────────────────────────────────────
// [P7.T1] TS anchor: coreSchemas.ts:SDKUserMessageSchema (L1290-1295)

// SDKUserPriority is the message delivery priority.
type SDKUserPriority string

const (
	SDKUserPriorityNow   SDKUserPriority = "now"
	SDKUserPriorityNext  SDKUserPriority = "next"
	SDKUserPriorityLater SDKUserPriority = "later"
)

// SDKUserTurnMessage is the TS-aligned top-level user message.
type SDKUserTurnMessage struct {
	Type            SDKMessageType  `json:"type"`    // "user"
	Message         interface{}     `json:"message"` // Anthropic API UserMessage
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	IsSynthetic     bool            `json:"isSynthetic,omitempty"`
	ToolUseResult   interface{}     `json:"tool_use_result,omitempty"`
	Priority        SDKUserPriority `json:"priority,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	UUID            string          `json:"uuid,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
}

// ── User replay message ────────────────────────────────────────────────────
// [P7.T1] TS anchor: coreSchemas.ts:SDKUserMessageReplaySchema (L1297-1303)

// SDKUserReplayMessage extends SDKUserTurnMessage with required uuid/session_id
// and the isReplay sentinel.
type SDKUserReplayMessage struct {
	Type            SDKMessageType  `json:"type"`    // "user"
	Message         interface{}     `json:"message"` // Anthropic API UserMessage
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	IsSynthetic     bool            `json:"isSynthetic,omitempty"`
	ToolUseResult   interface{}     `json:"tool_use_result,omitempty"`
	Priority        SDKUserPriority `json:"priority,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
	IsReplay        bool            `json:"isReplay"` // always true
}

// ── Stream event (partial assistant) message ────────────────────────────────
// [P7.T1] TS anchor: coreSchemas.ts:SDKPartialAssistantMessageSchema (L1496-1504)

// SDKStreamEventMessage wraps raw SSE stream events from the Anthropic API.
type SDKStreamEventMessage struct {
	Type            SDKMessageType `json:"type"`  // "stream_event"
	Event           interface{}    `json:"event"` // RawMessageStreamEvent
	ParentToolUseID *string        `json:"parent_tool_use_id"`
	UUID            string         `json:"uuid"`
	SessionID       string         `json:"session_id"`
}

// ── Result messages ────────────────────────────────────────────────────────

// SDKResultMessage is the union of success and error result messages.
type SDKResultMessage struct {
	Type              SDKMessageType         `json:"type"` // always "result"
	Subtype           SDKResultSubtype       `json:"subtype"`
	DurationMs        int                    `json:"duration_ms"`
	DurationAPIMs     int                    `json:"duration_api_ms"`
	IsError           bool                   `json:"is_error"`
	NumTurns          int                    `json:"num_turns"`
	Result            string                 `json:"result,omitempty"` // success only
	StopReason        *string                `json:"stop_reason"`
	TotalCostUSD      float64                `json:"total_cost_usd"`
	Usage             interface{}            `json:"usage"`
	ModelUsage        map[string]*ModelUsage `json:"modelUsage,omitempty"`
	PermissionDenials []SDKPermDenial        `json:"permission_denials"`
	Errors            []string               `json:"errors,omitempty"` // error only
	StructuredOutput  interface{}            `json:"structured_output,omitempty"`
	FastModeState     FastModeState          `json:"fast_mode_state,omitempty"`
	UUID              string                 `json:"uuid"`
	SessionID         string                 `json:"session_id"`
}

// ModelUsage tracks per-model usage stats.
type ModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
	MaxOutputTokens          int     `json:"maxOutputTokens"`
}

// SDKPermDenial is the SDK-level permission denial record.
type SDKPermDenial struct {
	ToolName  string                 `json:"tool_name"`
	ToolUseID string                 `json:"tool_use_id"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

// ── System init message ────────────────────────────────────────────────────

// SDKSystemInitMessage is the system/init envelope.
type SDKSystemInitMessage struct {
	Type              SDKMessageType    `json:"type"`    // "system"
	Subtype           SDKSystemSubtype  `json:"subtype"` // "init"
	Agents            []string          `json:"agents,omitempty"`
	APIKeySource      string            `json:"apiKeySource"`
	Betas             []string          `json:"betas,omitempty"`
	ClaudeCodeVersion string            `json:"claude_code_version"`
	CWD               string            `json:"cwd"`
	Tools             []string          `json:"tools"`
	MCPServers        []MCPServerStatus `json:"mcp_servers"`
	Model             string            `json:"model"`
	PermissionMode    string            `json:"permissionMode"`
	SlashCommands     []string          `json:"slash_commands"`
	OutputStyle       string            `json:"output_style"`
	Skills            []string          `json:"skills"`
	Plugins           []PluginInfo      `json:"plugins"`
	FastModeState     FastModeState     `json:"fast_mode_state,omitempty"`
	UUID              string            `json:"uuid"`
	SessionID         string            `json:"session_id"`
}

// MCPServerStatus describes a connected MCP server.
type MCPServerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// PluginInfo describes a loaded plugin.
type PluginInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
}

// ── Rate limit event ───────────────────────────────────────────────────────

// SDKRateLimitEvent carries rate limit state changes.
type SDKRateLimitEvent struct {
	Type          SDKMessageType `json:"type"` // "rate_limit_event"
	RateLimitInfo *RateLimitInfo `json:"rate_limit_info"`
	UUID          string         `json:"uuid"`
	SessionID     string         `json:"session_id"`
}

// RateLimitInfo carries rate limit details.
type RateLimitInfo struct {
	Status                string  `json:"status"` // "allowed", "allowed_warning", "rejected"
	ResetsAt              *int64  `json:"resetsAt,omitempty"`
	RateLimitType         string  `json:"rateLimitType,omitempty"`
	Utilization           float64 `json:"utilization,omitempty"`
	OverageStatus         string  `json:"overageStatus,omitempty"`
	OverageResetsAt       *int64  `json:"overageResetsAt,omitempty"`
	OverageDisabledReason string  `json:"overageDisabledReason,omitempty"`
	IsUsingOverage        bool    `json:"isUsingOverage,omitempty"`
	SurpassedThreshold    float64 `json:"surpassedThreshold,omitempty"`
}

// ── API retry message ──────────────────────────────────────────────────────

// SDKAPIRetryMessage is emitted when an API request fails and will be retried.
type SDKAPIRetryMessage struct {
	Type         SDKMessageType               `json:"type"`    // "system"
	Subtype      SDKSystemSubtype             `json:"subtype"` // "api_retry"
	Attempt      int                          `json:"attempt"`
	MaxRetries   int                          `json:"max_retries"`
	RetryDelayMs int                          `json:"retry_delay_ms"`
	ErrorStatus  *int                         `json:"error_status"`
	Error        SDKAssistantMessageErrorType `json:"error"`
	UUID         string                       `json:"uuid"`
	SessionID    string                       `json:"session_id"`
}

// ── Hook messages ──────────────────────────────────────────────────────────

// SDKHookStartedMessage is emitted when a hook starts.
type SDKHookStartedMessage struct {
	Type      SDKMessageType   `json:"type"`
	Subtype   SDKSystemSubtype `json:"subtype"` // "hook_started"
	HookID    string           `json:"hook_id"`
	HookName  string           `json:"hook_name"`
	HookEvent string           `json:"hook_event"`
	UUID      string           `json:"uuid"`
	SessionID string           `json:"session_id"`
}

// SDKHookProgressMessage carries incremental hook output.
type SDKHookProgressMessage struct {
	Type      SDKMessageType   `json:"type"`
	Subtype   SDKSystemSubtype `json:"subtype"` // "hook_progress"
	HookID    string           `json:"hook_id"`
	HookName  string           `json:"hook_name"`
	HookEvent string           `json:"hook_event"`
	Stdout    string           `json:"stdout"`
	Stderr    string           `json:"stderr"`
	Output    string           `json:"output"`
	UUID      string           `json:"uuid"`
	SessionID string           `json:"session_id"`
}

// SDKHookResponseMessage carries final hook outcome.
type SDKHookResponseMessage struct {
	Type      SDKMessageType   `json:"type"`
	Subtype   SDKSystemSubtype `json:"subtype"` // "hook_response"
	HookID    string           `json:"hook_id"`
	HookName  string           `json:"hook_name"`
	HookEvent string           `json:"hook_event"`
	Output    string           `json:"output"`
	Stdout    string           `json:"stdout"`
	Stderr    string           `json:"stderr"`
	ExitCode  *int             `json:"exit_code,omitempty"`
	Outcome   string           `json:"outcome"` // "success", "error", "cancelled"
	UUID      string           `json:"uuid"`
	SessionID string           `json:"session_id"`
}

// ── Tool progress ──────────────────────────────────────────────────────────

// SDKToolProgressMessage tracks tool execution progress.
type SDKToolProgressMessage struct {
	Type            SDKMessageType `json:"type"` // "tool_progress"
	ToolUseID       string         `json:"tool_use_id"`
	ToolName        string         `json:"tool_name"`
	ParentToolUseID *string        `json:"parent_tool_use_id"`
	ElapsedTimeSecs float64        `json:"elapsed_time_seconds"`
	TaskID          string         `json:"task_id,omitempty"`
	UUID            string         `json:"uuid"`
	SessionID       string         `json:"session_id"`
}

// ── Task messages ──────────────────────────────────────────────────────────

// SDKTaskStartedMessage is emitted when a background task starts.
type SDKTaskStartedMessage struct {
	Type         SDKMessageType   `json:"type"`
	Subtype      SDKSystemSubtype `json:"subtype"` // "task_started"
	TaskID       string           `json:"task_id"`
	ToolUseID    string           `json:"tool_use_id,omitempty"`
	Description  string           `json:"description"`
	TaskType     string           `json:"task_type,omitempty"`
	WorkflowName string           `json:"workflow_name,omitempty"`
	Prompt       string           `json:"prompt,omitempty"`
	UUID         string           `json:"uuid"`
	SessionID    string           `json:"session_id"`
}

// SDKTaskProgressMessage carries incremental task progress.
type SDKTaskProgressMessage struct {
	Type         SDKMessageType   `json:"type"`
	Subtype      SDKSystemSubtype `json:"subtype"` // "task_progress"
	TaskID       string           `json:"task_id"`
	ToolUseID    string           `json:"tool_use_id,omitempty"`
	Description  string           `json:"description"`
	Usage        *TaskUsage       `json:"usage"`
	LastToolName string           `json:"last_tool_name,omitempty"`
	Summary      string           `json:"summary,omitempty"`
	UUID         string           `json:"uuid"`
	SessionID    string           `json:"session_id"`
}

// SDKTaskNotificationMessage is emitted when a background task completes.
type SDKTaskNotificationMessage struct {
	Type       SDKMessageType   `json:"type"`
	Subtype    SDKSystemSubtype `json:"subtype"` // "task_notification"
	TaskID     string           `json:"task_id"`
	ToolUseID  string           `json:"tool_use_id,omitempty"`
	Status     string           `json:"status"` // "completed", "failed", "stopped"
	OutputFile string           `json:"output_file"`
	Summary    string           `json:"summary"`
	Usage      *TaskUsage       `json:"usage,omitempty"`
	UUID       string           `json:"uuid"`
	SessionID  string           `json:"session_id"`
}

// TaskUsage tracks token/tool usage for a background task.
type TaskUsage struct {
	TotalTokens int `json:"total_tokens"`
	ToolUses    int `json:"tool_uses"`
	DurationMs  int `json:"duration_ms"`
}

// ── Session state change ───────────────────────────────────────────────────

// SDKSessionStateChangedMessage mirrors notifySessionStateChanged.
type SDKSessionStateChangedMessage struct {
	Type      SDKMessageType   `json:"type"`
	Subtype   SDKSystemSubtype `json:"subtype"` // "session_state_changed"
	State     SDKSessionState  `json:"state"`
	UUID      string           `json:"uuid"`
	SessionID string           `json:"session_id"`
}

// ── Status message ─────────────────────────────────────────────────────────

// SDKStatusMessage carries session status (e.g. compacting).
type SDKStatusMessage struct {
	Type           SDKMessageType   `json:"type"`
	Subtype        SDKSystemSubtype `json:"subtype"` // "status"
	Status         *string          `json:"status"`  // "compacting" or null
	PermissionMode string           `json:"permissionMode,omitempty"`
	UUID           string           `json:"uuid"`
	SessionID      string           `json:"session_id"`
}

// ── Compact boundary ───────────────────────────────────────────────────────

// SDKCompactBoundaryMessage is the full SDK compact boundary message.
type SDKCompactBoundaryMessage struct {
	Type            SDKMessageType   `json:"type"`
	Subtype         SDKSystemSubtype `json:"subtype"` // "compact_boundary"
	CompactMetadata *CompactMetadata `json:"compact_metadata"`
	UUID            string           `json:"uuid"`
	SessionID       string           `json:"session_id"`
}

// CompactMetadata carries detailed compaction info.
type CompactMetadata struct {
	Trigger          string            `json:"trigger"` // "manual", "auto"
	PreTokens        int               `json:"pre_tokens"`
	PreservedSegment *PreservedSegment `json:"preserved_segment,omitempty"`
}

// PreservedSegment carries relink info for messagesToKeep.
type PreservedSegment struct {
	HeadUUID   string `json:"head_uuid"`
	AnchorUUID string `json:"anchor_uuid"`
	TailUUID   string `json:"tail_uuid"`
}

// ── Auth status ────────────────────────────────────────────────────────────

// SDKAuthStatusMessage carries authentication progress.
type SDKAuthStatusMessage struct {
	Type             SDKMessageType `json:"type"` // "auth_status"
	IsAuthenticating bool           `json:"isAuthenticating"`
	Output           []string       `json:"output"`
	Error            string         `json:"error,omitempty"`
	UUID             string         `json:"uuid"`
	SessionID        string         `json:"session_id"`
}

// ── Streamlined messages ───────────────────────────────────────────────────

// SDKStreamlinedTextMessage replaces SDKAssistantMessage in streamlined output.
type SDKStreamlinedTextMessage struct {
	Type      SDKMessageType `json:"type"` // "streamlined_text"
	Text      string         `json:"text"`
	SessionID string         `json:"session_id"`
	UUID      string         `json:"uuid"`
}

// SDKStreamlinedToolUseSummaryMessage replaces tool_use blocks in streamlined output.
type SDKStreamlinedToolUseSummaryMessage struct {
	Type        SDKMessageType `json:"type"` // "streamlined_tool_use_summary"
	ToolSummary string         `json:"tool_summary"`
	SessionID   string         `json:"session_id"`
	UUID        string         `json:"uuid"`
}

// ── Post-turn summary ──────────────────────────────────────────────────────

// SDKPostTurnSummaryMessage is an internal background summary.
type SDKPostTurnSummaryMessage struct {
	Type           SDKMessageType   `json:"type"`
	Subtype        SDKSystemSubtype `json:"subtype"` // "post_turn_summary"
	SummarizesUUID string           `json:"summarizes_uuid"`
	StatusCategory string           `json:"status_category"` // blocked, waiting, completed, review_ready, failed
	StatusDetail   string           `json:"status_detail"`
	IsNoteworthy   bool             `json:"is_noteworthy"`
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	RecentAction   string           `json:"recent_action"`
	NeedsAction    string           `json:"needs_action"`
	ArtifactURLs   []string         `json:"artifact_urls"`
	UUID           string           `json:"uuid"`
	SessionID      string           `json:"session_id"`
}

// ── Prompt suggestion ──────────────────────────────────────────────────────

// SDKPromptSuggestionMessage carries predicted next user prompts.
type SDKPromptSuggestionMessage struct {
	Type       SDKMessageType `json:"type"` // "prompt_suggestion"
	Suggestion string         `json:"suggestion"`
	UUID       string         `json:"uuid"`
	SessionID  string         `json:"session_id"`
}

// ── Tool use summary (SDK-level) ───────────────────────────────────────────

// SDKToolUseSummaryMessage is the SDK-level tool use summary.
type SDKToolUseSummaryMessage struct {
	Type                SDKMessageType `json:"type"` // "tool_use_summary"
	Summary             string         `json:"summary"`
	PrecedingToolUseIDs []string       `json:"preceding_tool_use_ids"`
	UUID                string         `json:"uuid"`
	SessionID           string         `json:"session_id"`
}

// ── Elicitation complete ───────────────────────────────────────────────────

// SDKElicitationCompleteMessage is emitted when an MCP elicitation is done.
type SDKElicitationCompleteMessage struct {
	Type          SDKMessageType   `json:"type"`
	Subtype       SDKSystemSubtype `json:"subtype"` // "elicitation_complete"
	MCPServerName string           `json:"mcp_server_name"`
	ElicitationID string           `json:"elicitation_id"`
	UUID          string           `json:"uuid"`
	SessionID     string           `json:"session_id"`
}

// ── Local command output ───────────────────────────────────────────────────

// SDKLocalCommandOutputMessage carries slash command output.
type SDKLocalCommandOutputMessage struct {
	Type      SDKMessageType   `json:"type"`
	Subtype   SDKSystemSubtype `json:"subtype"` // "local_command_output"
	Content   string           `json:"content"`
	UUID      string           `json:"uuid"`
	SessionID string           `json:"session_id"`
}

// ── Files persisted ────────────────────────────────────────────────────────

// SDKFilesPersistedEvent is emitted after file persistence.
type SDKFilesPersistedEvent struct {
	Type        SDKMessageType   `json:"type"`
	Subtype     SDKSystemSubtype `json:"subtype"` // "files_persisted"
	Files       []PersistedFile  `json:"files"`
	Failed      []FailedFile     `json:"failed"`
	ProcessedAt string           `json:"processed_at"`
	UUID        string           `json:"uuid"`
	SessionID   string           `json:"session_id"`
}

// PersistedFile describes a successfully persisted file.
type PersistedFile struct {
	Filename string `json:"filename"`
	FileID   string `json:"file_id"`
}

// FailedFile describes a file that failed to persist.
type FailedFile struct {
	Filename string `json:"filename"`
	Error    string `json:"error"`
}
