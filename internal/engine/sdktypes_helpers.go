package engine

import "github.com/google/uuid"

// [P7.T3] SDK message construction helpers.

// NewSDKUUID generates a new UUID string for SDK messages.
func NewSDKUUID() string {
	return uuid.New().String()
}

// NewSDKResultSuccess creates a success result message.
func NewSDKResultSuccess(sessionID string, result string, durationMs, durationAPIMs, numTurns int, totalCostUSD float64, usage interface{}, stopReason string) *SDKResultMessage {
	sr := stopReason
	return &SDKResultMessage{
		Type:         SDKMsgResult,
		Subtype:      SDKResultSuccess,
		DurationMs:   durationMs,
		DurationAPIMs: durationAPIMs,
		IsError:      false,
		NumTurns:     numTurns,
		Result:       result,
		StopReason:   &sr,
		TotalCostUSD: totalCostUSD,
		Usage:        usage,
		UUID:         NewSDKUUID(),
		SessionID:    sessionID,
	}
}

// NewSDKResultError creates an error result message.
func NewSDKResultError(sessionID string, subtype SDKResultSubtype, errors []string, durationMs, durationAPIMs, numTurns int, totalCostUSD float64, usage interface{}) *SDKResultMessage {
	return &SDKResultMessage{
		Type:         SDKMsgResult,
		Subtype:      subtype,
		DurationMs:   durationMs,
		DurationAPIMs: durationAPIMs,
		IsError:      true,
		NumTurns:     numTurns,
		TotalCostUSD: totalCostUSD,
		Usage:        usage,
		Errors:       errors,
		UUID:         NewSDKUUID(),
		SessionID:    sessionID,
	}
}

// NewSDKSystemInit creates a system/init message.
func NewSDKSystemInit(sessionID string, opts SDKSystemInitMessage) *SDKSystemInitMessage {
	opts.Type = SDKMsgSystem
	opts.Subtype = SDKSystemSubtypeInit
	opts.SessionID = sessionID
	if opts.UUID == "" {
		opts.UUID = NewSDKUUID()
	}
	return &opts
}

// NewSDKAPIRetry creates an API retry message.
func NewSDKAPIRetry(sessionID string, attempt, maxRetries, retryDelayMs int, errorStatus *int, errType SDKAssistantMessageErrorType) *SDKAPIRetryMessage {
	return &SDKAPIRetryMessage{
		Type:         SDKMsgSystem,
		Subtype:      SDKSystemSubtypeAPIRetry,
		Attempt:      attempt,
		MaxRetries:   maxRetries,
		RetryDelayMs: retryDelayMs,
		ErrorStatus:  errorStatus,
		Error:        errType,
		UUID:         NewSDKUUID(),
		SessionID:    sessionID,
	}
}

// NewSDKSessionStateChanged creates a session state change message.
func NewSDKSessionStateChanged(sessionID string, state SDKSessionState) *SDKSessionStateChangedMessage {
	return &SDKSessionStateChangedMessage{
		Type:      SDKMsgSystem,
		Subtype:   SDKSystemSubtypeSessionStateChanged,
		State:     state,
		UUID:      NewSDKUUID(),
		SessionID: sessionID,
	}
}

// NewSDKStatusMessage creates a status message.
func NewSDKStatusMessage(sessionID string, status *string, permMode string) *SDKStatusMessage {
	return &SDKStatusMessage{
		Type:           SDKMsgSystem,
		Subtype:        SDKSystemSubtypeStatus,
		Status:         status,
		PermissionMode: permMode,
		UUID:           NewSDKUUID(),
		SessionID:      sessionID,
	}
}

// NewSDKTaskStarted creates a task started message.
func NewSDKTaskStarted(sessionID, taskID, description string) *SDKTaskStartedMessage {
	return &SDKTaskStartedMessage{
		Type:        SDKMsgSystem,
		Subtype:     SDKSystemSubtypeTaskStarted,
		TaskID:      taskID,
		Description: description,
		UUID:        NewSDKUUID(),
		SessionID:   sessionID,
	}
}

// NewSDKTaskNotification creates a task completion notification.
func NewSDKTaskNotification(sessionID, taskID, status, outputFile, summary string) *SDKTaskNotificationMessage {
	return &SDKTaskNotificationMessage{
		Type:       SDKMsgSystem,
		Subtype:    SDKSystemSubtypeTaskNotification,
		TaskID:     taskID,
		Status:     status,
		OutputFile: outputFile,
		Summary:    summary,
		UUID:       NewSDKUUID(),
		SessionID:  sessionID,
	}
}

// NewSDKHookStarted creates a hook started message.
func NewSDKHookStarted(sessionID, hookID, hookName, hookEvent string) *SDKHookStartedMessage {
	return &SDKHookStartedMessage{
		Type:      SDKMsgSystem,
		Subtype:   SDKSystemSubtypeHookStarted,
		HookID:    hookID,
		HookName:  hookName,
		HookEvent: hookEvent,
		UUID:      NewSDKUUID(),
		SessionID: sessionID,
	}
}

// NewSDKToolProgress creates a tool progress message.
func NewSDKToolProgress(sessionID, toolUseID, toolName string, elapsedSecs float64) *SDKToolProgressMessage {
	return &SDKToolProgressMessage{
		Type:            SDKMsgToolProgress,
		ToolUseID:       toolUseID,
		ToolName:        toolName,
		ElapsedTimeSecs: elapsedSecs,
		UUID:            NewSDKUUID(),
		SessionID:       sessionID,
	}
}
