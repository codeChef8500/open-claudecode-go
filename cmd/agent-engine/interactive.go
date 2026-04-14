package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wall-ai/agent-engine/internal/agent"
	agentswarm "github.com/wall-ai/agent-engine/internal/agent/swarm"
	"github.com/wall-ai/agent-engine/internal/buddy"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/session"
	"github.com/wall-ai/agent-engine/internal/tool/askuser"
	"github.com/wall-ai/agent-engine/internal/tool/sendmessage"
	"github.com/wall-ai/agent-engine/internal/tui"
	"github.com/wall-ai/agent-engine/internal/util"
)

// runInteractiveMode launches the full-screen Bubbletea TUI.
func runInteractiveMode(ctx context.Context, appCfg *util.AppConfig, wd string) error {
	// ── Session restore: resolve session ID from --continue / --resume ───
	var restoreResult *session.RestoreResult
	restoredSessionID := appCfg.ResumeSessionID

	if appCfg.ContinueSession && restoredSessionID == "" {
		// --continue: find the most recent session
		store := session.NewStorage(session.DefaultStorageDir())
		latestID, err := store.LatestSessionID()
		if err != nil {
			slog.Warn("session continue: failed to list sessions", slog.Any("err", err))
		} else if latestID == "" {
			slog.Info("session continue: no previous sessions found")
		} else {
			restoredSessionID = latestID
		}
	}

	if restoredSessionID != "" {
		store := session.NewStorage(session.DefaultStorageDir())
		rr, err := store.RestoreSession(restoredSessionID)
		if err != nil {
			slog.Warn("session restore failed", slog.String("id", restoredSessionID), slog.Any("err", err))
		} else {
			restoreResult = rr
			if warnings := session.ValidateRestore(rr); len(warnings) > 0 {
				for _, w := range warnings {
					slog.Warn("session restore warning", slog.String("warning", w))
				}
			}
			slog.Info("session restore loaded",
				slog.String("id", restoredSessionID),
				slog.Int("messages", len(rr.Messages)))
		}
	}

	// Use restored session ID for bootstrap so the engine reuses it.
	bootstrapSessionID := ""
	if restoreResult != nil {
		bootstrapSessionID = restoredSessionID
	}

	// ── Match coordinator mode to restored session ─────────────────────
	// Aligned with TS sessionRestore.ts:427-433 matchSessionMode().
	if restoreResult != nil && restoreResult.Mode != "" {
		if warning := agent.MatchSessionMode(agent.CoordinatorSessionMode(restoreResult.Mode)); warning != "" {
			slog.Info("session restore: mode switch", slog.String("warning", warning))
		}
	}

	result, err := session.Bootstrap(ctx, session.BootstrapConfig{
		AppConfig: appCfg,
		WorkDir:   wd,
		SessionID: bootstrapSessionID,
	})
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	defer session.Shutdown(result)

	// ── Persist current mode for session resume ─────────────────────────
	// Aligned with TS main.tsx:3770-3772 saveMode().
	store := session.NewStorage(session.DefaultStorageDir())
	currentMode := "normal"
	if agent.IsCoordinatorMode() {
		currentMode = "coordinator"
	}
	if err := store.SaveMode(result.Engine.SessionID(), currentMode); err != nil {
		slog.Warn("failed to persist session mode", slog.Any("err", err))
	}

	// Seed engine history from restored messages so the LLM has context.
	if restoreResult != nil && len(restoreResult.Messages) > 0 {
		result.Engine.SeedHistory(restoreResult.Messages)
	}

	runner := session.NewRunner(result)

	// Wire interactive callbacks on the engine so tools like AskUserQuestion
	// can present TUI dialogs and block waiting for user responses.
	// We need a reference to the program for sending messages from goroutines.
	var program *tea.Program

	// RequestPrompt: tools call this to show structured UI dialogs (e.g. AskUserQuestion).
	result.Engine.SetRequestPrompt(func(sourceName string, toolInputSummary string) func(request interface{}) (interface{}, error) {
		return func(request interface{}) (interface{}, error) {
			reqMap, ok := request.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("AskUserQuestion: expected map request, got %T", request)
			}

			// Extract questions from the request.
			// The tool passes []askuser.Question directly, but Go does not
			// allow []T → []interface{} assertion, so we handle both types.
			questionsRaw, _ := reqMap["questions"]
			var questions []interface{}
			switch qs := questionsRaw.(type) {
			case []askuser.Question:
				for _, q := range qs {
					questions = append(questions, q)
				}
			case []interface{}:
				for _, q := range qs {
					data, _ := json.Marshal(q)
					var aq askuser.Question
					if err := json.Unmarshal(data, &aq); err == nil {
						questions = append(questions, aq)
					}
				}
			default:
				// Fallback: JSON round-trip for any other slice type.
				data, _ := json.Marshal(questionsRaw)
				var typed []askuser.Question
				if json.Unmarshal(data, &typed) == nil {
					for _, q := range typed {
						questions = append(questions, q)
					}
				}
			}

			// Create result channel and send request to TUI.
			resultCh := make(chan interface{}, 1)
			if program != nil {
				program.Send(tui.AskQuestionRequestMsg{
					Questions: questions,
					ResultCh:  resultCh,
				})
			} else {
				return "no interactive program available", nil
			}

			// Block waiting for user response.
			resp := <-resultCh
			return resp, nil
		}
	})

	app, err := tui.NewApp(tui.AppConfig{
		Dark:           appCfg.DarkMode,
		Model:          appCfg.Model,
		PermissionMode: appCfg.PermissionMode,
		WorkDir:        wd,
		SubmitFn: func(text string) {
			// Run engine interaction in a goroutine so the TUI stays responsive.
			go func() {
				// BUG-1 fix: recover from panics so the TUI doesn't crash.
				defer func() {
					if r := recover(); r != nil {
						slog.Error("panic in command handler", slog.Any("error", r))
						if program != nil {
							program.Send(tui.StreamErrorMsg{
								Err: fmt.Errorf("internal error: %v", r),
							})
							program.Send(tui.StreamDoneMsg{})
						}
					}
				}()
				handleInteractiveInput(ctx, runner, program, text)
			}()
		},
	})
	if err != nil {
		return fmt.Errorf("create TUI: %w", err)
	}

	program = tea.NewProgram(app, tea.WithAltScreen())

	// BUG-7 fix: wire callbacks once to avoid per-submission data race.
	wireRunnerCallbacks(runner, program)
	if result.PermBridge != nil {
		result.PermBridge.SetOnRequest(func(req *agentswarm.PermissionBridgeRequest) {
			if req == nil || program == nil {
				return
			}
			program.Send(tui.TeammatePermissionRequestMsg{
				WorkerName:  req.WorkerName,
				WorkerColor: req.WorkerColor,
				ToolName:    req.ToolName,
				Description: req.Description,
				RequestID:   req.RequestID,
				ResponseCh:  req.ResponseCh,
			})
		})
	}
	for _, smTool := range result.SendMessageTools {
		if smTool == nil {
			continue
		}
		smTool.SetStructuredSendCallback(func(ev sendmessage.StructuredSendEvent) {
			if program == nil {
				return
			}
			name := ev.From
			if parsed, _ := agentswarm.ParseAgentID(ev.From); parsed != "" {
				name = parsed
			}
			switch ev.MessageType {
			case "shutdown_request":
				program.Send(tui.TeammateMessageMsg{
					From:         name,
					Content:      fmt.Sprintf("requested shutdown for %s: %s", shortAgentName(ev.To), strings.TrimSpace(ev.Message)),
					IsStructured: true,
				})
			case "shutdown_approved", "shutdown_response":
				approved := ev.Approved == nil || *ev.Approved
				if approved {
					program.Send(tui.TeammateShutdownMsg{
						Name:    shortAgentName(ev.To),
						AgentID: ev.To,
						Reason:  strings.TrimSpace(ev.Message),
					})
				} else {
					program.Send(tui.TeammateMessageMsg{
						From:         shortAgentName(ev.To),
						Content:      fmt.Sprintf("rejected shutdown: %s", strings.TrimSpace(ev.Message)),
						IsStructured: true,
					})
				}
			case "shutdown_rejected":
				program.Send(tui.TeammateMessageMsg{
					From:         shortAgentName(ev.To),
					Content:      fmt.Sprintf("rejected shutdown: %s", strings.TrimSpace(ev.Message)),
					IsStructured: true,
				})
			}
		})
	}

	// Start idle notification poller so coordinator receives task-notifications
	// even while waiting for user input (aligned with TS idle drain).
	pollerCtx, pollerCancel := context.WithCancel(ctx)
	defer pollerCancel()
	runner.StartNotificationPoller(pollerCtx, 1*time.Second)

	// P3+P5: Auto-load companion on startup; auto-hatch if none exists
	configDir := session.BuddyConfigDir()
	userID := buddy.GetOrCreateUserID(configDir)
	comp := buddy.LoadCompanion(userID, configDir)
	if comp == nil {
		// Auto-hatch on first launch (no manual /buddy required)
		comp = buddy.HatchWithoutLLM(userID)
		if comp != nil {
			_ = buddy.SaveCompanion(comp, configDir)
		}
	}
	if comp != nil {
		app.SetCompanion(comp)
		app.SetCompanionMuted(buddy.IsCompanionMuted(configDir))

		// P1: Create observer for companion reactions → TUI
		obs := buddy.NewObserver(comp, func(text string) {
			program.Send(tui.CompanionReactionMsg{Text: text})
		})
		runner.SetObserver(obs)
	}

	// ── Send restored session history to TUI ────────────────────────────
	if restoreResult != nil && len(restoreResult.Messages) > 0 {
		chatMsgs := tui.MessagesToChat(restoreResult.Messages)
		go func() {
			program.Send(tui.RestoreMsg{
				Messages:  chatMsgs,
				SessionID: restoredSessionID,
				Summary:   restoreResult.SummaryText,
			})
		}()
	}

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	return nil
}

// wireRunnerCallbacks sets up all runner → TUI callbacks once.
// BUG-7 fix: wire callbacks once at setup time instead of per-submission
// to eliminate the data race on runner callback fields.
func wireRunnerCallbacks(runner *session.Runner, p *tea.Program) {
	runner.OnTextDelta = func(t string) {
		p.Send(tui.StreamTextMsg{Text: t})
	}
	runner.OnToolStart = func(id, name, input string) {
		p.Send(tui.ToolStartMsg{ToolID: id, ToolName: name, Input: input})
	}
	runner.OnToolDone = func(id, name, output string, isError bool) {
		p.Send(tui.ToolDoneMsg{ToolID: id, ToolName: name, Output: output, IsError: isError})
	}
	runner.OnToolProgress = func(id, name, content string) {
		p.Send(tui.ToolProgressMsg{ToolID: id, ToolName: name, Content: content})
	}
	runner.OnDone = func() {
		p.Send(tui.StreamDoneMsg{})
	}
	runner.OnError = func(err error) {
		p.Send(tui.StreamErrorMsg{Err: err})
	}
	runner.OnSystem = func(t string) {
		p.Send(tui.SystemMsg{Text: t})
	}
	runner.OnClearHistory = func() {
		p.Send(tui.ClearHistoryMsg{})
	}
	runner.OnCompact = func() {
		p.Send(tui.CompactHistoryMsg{})
	}

	// Companion callbacks → TUI state sync
	runner.OnCompanionLoad = func(comp *buddy.Companion) {
		p.Send(tui.CompanionLoadMsg{Companion: comp})
	}
	runner.OnCompanionPet = func(tsMs int64) {
		p.Send(tui.CompanionPetMsg{Timestamp: tsMs})
	}
	runner.OnCompanionMute = func(muted bool) {
		p.Send(tui.CompanionMuteMsg{Muted: muted})
	}
	runner.OnCompanionReaction = func(text string) {
		p.Send(tui.CompanionReactionMsg{Text: text})
	}
}

// handleInteractiveInput processes a user message through the engine runner
// and forwards streaming events back to the TUI via tea.Program.Send.
func handleInteractiveInput(ctx context.Context, runner *session.Runner, p *tea.Program, text string) {
	if p == nil {
		return
	}

	if !runner.HandleInput(ctx, text) {
		p.Send(tea.Quit())
	}
}

// ── Tool event message types (TUI-level) ────────────────────────────────────

// formatToolInput returns a summary of tool input for display.
func formatToolInput(ev *engine.StreamEvent) string {
	if ev.ToolInput == nil {
		return ""
	}
	data, err := json.Marshal(ev.ToolInput)
	if err != nil {
		return fmt.Sprintf("%v", ev.ToolInput)
	}
	return string(data)
}

func shortAgentName(agentID string) string {
	if name, _ := agentswarm.ParseAgentID(agentID); name != "" {
		return name
	}
	return agentID
}
