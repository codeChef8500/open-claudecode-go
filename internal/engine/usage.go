package engine

// ────────────────────────────────────────────────────────────────────────────
// [P7.T2] NonNullableUsage + UpdateUsage + AccumulateUsage
// TS anchors: services/api/emptyUsage.ts, services/api/claude.ts:L2924-3038
// ────────────────────────────────────────────────────────────────────────────

// ServerToolUse tracks server-side tool usage counters.
type ServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests"`
	WebFetchRequests  int `json:"web_fetch_requests"`
}

// CacheCreation tracks ephemeral cache creation tokens.
type CacheCreation struct {
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
}

// Iteration captures per-iteration metadata from the API response.
type Iteration struct {
	UsageOutputTokens int    `json:"usage_output_tokens,omitempty"`
	ModelID           string `json:"model_id,omitempty"`
}

// NonNullableUsage mirrors the TS NonNullableUsage type exactly.
// All fields are guaranteed non-null (zero-initialized via EmptyUsage).
type NonNullableUsage struct {
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
	CacheDeletedInputTokens  int            `json:"cache_deleted_input_tokens,omitempty"`
	ServerToolUse            ServerToolUse  `json:"server_tool_use"`
	ServiceTier              string         `json:"service_tier"`
	CacheCreation            CacheCreation  `json:"cache_creation"`
	InferenceGeo             string         `json:"inference_geo"`
	Iterations               []Iteration    `json:"iterations"`
	Speed                    string         `json:"speed"`
}

// EmptyUsage returns a zero-initialized NonNullableUsage.
// Mirrors TS EMPTY_USAGE from services/api/emptyUsage.ts.
func EmptyUsage() NonNullableUsage {
	return NonNullableUsage{
		ServerToolUse: ServerToolUse{},
		ServiceTier:   "standard",
		CacheCreation: CacheCreation{},
		InferenceGeo:  "",
		Iterations:    []Iteration{},
		Speed:         "standard",
	}
}

// PartialUsage represents the partial usage data from message_start or
// message_delta events. Fields are pointers to distinguish "absent" from "zero".
// Mirrors BetaMessageDeltaUsage / BetaUsage from the Anthropic SDK.
type PartialUsage struct {
	InputTokens              *int           `json:"input_tokens,omitempty"`
	OutputTokens             *int           `json:"output_tokens,omitempty"`
	CacheCreationInputTokens *int           `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int           `json:"cache_read_input_tokens,omitempty"`
	CacheDeletedInputTokens  *int           `json:"cache_deleted_input_tokens,omitempty"`
	ServerToolUse            *ServerToolUse `json:"server_tool_use,omitempty"`
	CacheCreation            *CacheCreation `json:"cache_creation,omitempty"`
	Iterations               []Iteration    `json:"iterations,omitempty"`
	Speed                    string         `json:"speed,omitempty"`
}

// UpdateUsage conditionally merges partial usage data into an existing usage.
// Only non-null, non-zero token fields overwrite existing values (TS semantics).
// output_tokens uses null-coalesce semantics (overwrites even with 0).
// Mirrors TS updateUsage from services/api/claude.ts:L2924-2986.
func UpdateUsage(usage NonNullableUsage, part *PartialUsage) NonNullableUsage {
	if part == nil {
		return usage
	}

	result := usage

	// input_tokens: only update if non-null and > 0
	if part.InputTokens != nil && *part.InputTokens > 0 {
		result.InputTokens = *part.InputTokens
	}

	// cache_creation_input_tokens: only update if non-null and > 0
	if part.CacheCreationInputTokens != nil && *part.CacheCreationInputTokens > 0 {
		result.CacheCreationInputTokens = *part.CacheCreationInputTokens
	}

	// cache_read_input_tokens: only update if non-null and > 0
	if part.CacheReadInputTokens != nil && *part.CacheReadInputTokens > 0 {
		result.CacheReadInputTokens = *part.CacheReadInputTokens
	}

	// output_tokens: null-coalesce (overwrite with any non-nil value)
	if part.OutputTokens != nil {
		result.OutputTokens = *part.OutputTokens
	}

	// server_tool_use: null-coalesce per-field
	if part.ServerToolUse != nil {
		result.ServerToolUse.WebSearchRequests = part.ServerToolUse.WebSearchRequests
		result.ServerToolUse.WebFetchRequests = part.ServerToolUse.WebFetchRequests
	}

	// cache_creation: null-coalesce per-field
	if part.CacheCreation != nil {
		result.CacheCreation.Ephemeral1hInputTokens = part.CacheCreation.Ephemeral1hInputTokens
		result.CacheCreation.Ephemeral5mInputTokens = part.CacheCreation.Ephemeral5mInputTokens
	}

	// cache_deleted_input_tokens: only update if non-null and > 0
	if part.CacheDeletedInputTokens != nil && *part.CacheDeletedInputTokens > 0 {
		result.CacheDeletedInputTokens = *part.CacheDeletedInputTokens
	}

	// iterations: null-coalesce
	if part.Iterations != nil {
		result.Iterations = part.Iterations
	}

	// speed: null-coalesce
	if part.Speed != "" {
		result.Speed = part.Speed
	}

	return result
}

// AccumulateUsage adds one message's usage into a running total.
// Used to track cumulative usage across multiple assistant turns.
// Mirrors TS accumulateUsage from services/api/claude.ts:L2993-3038.
func AccumulateUsage(total, message NonNullableUsage) NonNullableUsage {
	return NonNullableUsage{
		InputTokens:              total.InputTokens + message.InputTokens,
		OutputTokens:             total.OutputTokens + message.OutputTokens,
		CacheCreationInputTokens: total.CacheCreationInputTokens + message.CacheCreationInputTokens,
		CacheReadInputTokens:     total.CacheReadInputTokens + message.CacheReadInputTokens,
		CacheDeletedInputTokens:  total.CacheDeletedInputTokens + message.CacheDeletedInputTokens,
		ServerToolUse: ServerToolUse{
			WebSearchRequests: total.ServerToolUse.WebSearchRequests + message.ServerToolUse.WebSearchRequests,
			WebFetchRequests:  total.ServerToolUse.WebFetchRequests + message.ServerToolUse.WebFetchRequests,
		},
		ServiceTier: message.ServiceTier, // use most recent
		CacheCreation: CacheCreation{
			Ephemeral1hInputTokens: total.CacheCreation.Ephemeral1hInputTokens + message.CacheCreation.Ephemeral1hInputTokens,
			Ephemeral5mInputTokens: total.CacheCreation.Ephemeral5mInputTokens + message.CacheCreation.Ephemeral5mInputTokens,
		},
		InferenceGeo: message.InferenceGeo, // use most recent
		Iterations:   message.Iterations,   // use most recent
		Speed:        message.Speed,         // use most recent
	}
}

// UsageToNonNullable converts the legacy UsageStats to NonNullableUsage.
func UsageToNonNullable(u *UsageStats) NonNullableUsage {
	if u == nil {
		return EmptyUsage()
	}
	nnu := EmptyUsage()
	nnu.InputTokens = u.InputTokens
	nnu.OutputTokens = u.OutputTokens
	nnu.CacheCreationInputTokens = u.CacheCreationInputTokens
	nnu.CacheReadInputTokens = u.CacheReadInputTokens
	nnu.CacheDeletedInputTokens = u.CacheDeletedInputTokens
	return nnu
}

// NonNullableToUsageStats converts NonNullableUsage back to legacy UsageStats.
func NonNullableToUsageStats(nnu NonNullableUsage) *UsageStats {
	return &UsageStats{
		InputTokens:              nnu.InputTokens,
		OutputTokens:             nnu.OutputTokens,
		CacheCreationInputTokens: nnu.CacheCreationInputTokens,
		CacheReadInputTokens:     nnu.CacheReadInputTokens,
		CacheDeletedInputTokens:  nnu.CacheDeletedInputTokens,
	}
}
