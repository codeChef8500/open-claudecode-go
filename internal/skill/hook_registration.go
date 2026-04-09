package skill

import (
	"fmt"
	"log/slog"

	"github.com/wall-ai/agent-engine/internal/hooks"
)

// Hook registration from skill frontmatter — aligned with claude-code-main
// loadSkillsDir.ts hook registration logic.
//
// When a skill is loaded and has a `hooks` section in its frontmatter, the
// hooks are automatically registered in the session-scoped hook store.

// RegisterSkillHooks extracts hook configurations from a skill's frontmatter
// and registers them in the given SessionHookStore. Returns the number of
// hooks registered.
func RegisterSkillHooks(skill *Skill, store *hooks.SessionHookStore) int {
	if skill == nil || store == nil {
		return 0
	}

	hooksMap := skill.Meta.Hooks
	if len(hooksMap) == 0 {
		return 0
	}

	count := 0
	source := fmt.Sprintf("skill:%s", skill.Meta.Name)

	for eventName, hookCfgRaw := range hooksMap {
		event := hooks.HookEvent(eventName)

		// hookCfgRaw can be:
		//   string  → a shell command
		//   map     → a full HookConfig
		//   []any   → a list of hooks for this event

		switch v := hookCfgRaw.(type) {
		case string:
			// Simple string: treat as a command hook.
			store.Add(event, hooks.HookConfig{
				Type:    hooks.HookTypeCommand,
				Command: v,
				Source:  source,
			})
			count++

		case map[string]interface{}:
			cfg := parseHookConfigMap(v, source)
			store.Add(event, cfg)
			count++

		case map[interface{}]interface{}:
			// YAML sometimes produces map[interface{}]interface{}.
			converted := make(map[string]interface{})
			for k, val := range v {
				if ks, ok := k.(string); ok {
					converted[ks] = val
				}
			}
			cfg := parseHookConfigMap(converted, source)
			store.Add(event, cfg)
			count++

		case []interface{}:
			for _, item := range v {
				switch iv := item.(type) {
				case string:
					store.Add(event, hooks.HookConfig{
						Type:    hooks.HookTypeCommand,
						Command: iv,
						Source:  source,
					})
					count++
				case map[string]interface{}:
					cfg := parseHookConfigMap(iv, source)
					store.Add(event, cfg)
					count++
				}
			}

		default:
			slog.Debug("skill hook: unsupported hook config type",
				"skill", skill.Meta.Name,
				"event", eventName,
				"type", fmt.Sprintf("%T", hookCfgRaw))
		}
	}

	if count > 0 {
		slog.Debug("registered skill hooks",
			"skill", skill.Meta.Name,
			"count", count)
	}

	return count
}

// RegisterAllSkillHooks registers hooks from all skills in the list.
func RegisterAllSkillHooks(skills []*Skill, store *hooks.SessionHookStore) int {
	total := 0
	for _, s := range skills {
		total += RegisterSkillHooks(s, store)
	}
	return total
}

// UnregisterSkillHooks removes all hooks registered by a specific skill.
func UnregisterSkillHooks(skillName string, store *hooks.SessionHookStore) {
	source := fmt.Sprintf("skill:%s", skillName)
	for _, event := range hooks.AllHookEvents {
		store.Remove(event, source)
	}
}

// parseHookConfigMap converts a map to a HookConfig.
func parseHookConfigMap(m map[string]interface{}, source string) hooks.HookConfig {
	cfg := hooks.HookConfig{
		Source: source,
	}

	if t, ok := m["type"].(string); ok {
		cfg.Type = hooks.HookType(t)
	}
	if cmd, ok := m["command"].(string); ok {
		cfg.Command = cmd
	}
	if url, ok := m["url"].(string); ok {
		cfg.URL = url
	}
	if method, ok := m["method"].(string); ok {
		cfg.Method = method
	}
	if tmpl, ok := m["prompt_template"].(string); ok {
		cfg.PromptTemplate = tmpl
	}
	if async, ok := m["async"].(bool); ok {
		cfg.Async = async
	}
	if timeout, ok := m["timeout_seconds"].(int); ok {
		cfg.TimeoutSeconds = timeout
	}
	if args, ok := m["args"].([]interface{}); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				cfg.Args = append(cfg.Args, s)
			}
		}
	}
	if env, ok := m["env"].(map[string]interface{}); ok {
		cfg.Env = make(map[string]string)
		for k, v := range env {
			if s, ok := v.(string); ok {
				cfg.Env[k] = s
			}
		}
	}
	if headers, ok := m["headers"].(map[string]interface{}); ok {
		cfg.Headers = make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				cfg.Headers[k] = s
			}
		}
	}

	// Default type to command if a command is specified.
	if cfg.Type == "" && cfg.Command != "" {
		cfg.Type = hooks.HookTypeCommand
	}
	if cfg.Type == "" && cfg.URL != "" {
		cfg.Type = hooks.HookTypeHTTP
	}
	if cfg.Type == "" && cfg.PromptTemplate != "" {
		cfg.Type = hooks.HookTypePrompt
	}

	return cfg
}
