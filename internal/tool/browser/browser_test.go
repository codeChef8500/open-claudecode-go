package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewBrowserTool(t *testing.T) {
	bt := New()
	if bt == nil {
		t.Fatal("New() returned nil")
	}
	if bt.Name() != "BrowserDrission" {
		t.Errorf("Name() = %q, want %q", bt.Name(), "BrowserDrission")
	}
	if bt.UserFacingName() != "Browser" {
		t.Errorf("UserFacingName() = %q, want %q", bt.UserFacingName(), "Browser")
	}
	if bt.Description() == "" {
		t.Error("Description() is empty")
	}
}

func TestInputSchema(t *testing.T) {
	schema := inputSchema()
	if len(schema) == 0 {
		t.Fatal("inputSchema() is empty")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("inputSchema() is not valid JSON: %v", err)
	}
	props, ok := parsed["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema has no properties")
	}
	if _, ok := props["action"]; !ok {
		t.Error("schema missing 'action' property")
	}
}

func TestValidateInput(t *testing.T) {
	bt := New()
	ctx := context.Background()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid action", `{"action":"list_sessions"}`, false},
		{"empty action", `{"action":""}`, true},
		{"missing action", `{}`, true},
		{"invalid json", `{bad}`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := bt.ValidateInput(ctx, json.RawMessage(tc.input))
			if tc.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLocatorResolve(t *testing.T) {
	tests := []struct {
		input    string
		strategy LocatorStrategy
		contains string
	}{
		{"#myid", StrategyCSS, "#myid"},
		{".myclass", StrategyCSS, ".myclass"},
		{"css=div > span", StrategyCSS, "div > span"},
		{"c=.foo", StrategyCSS, ".foo"},
		{"//div[@id='x']", StrategyXPath, "//div[@id='x']"},
		{"xpath=//a", StrategyXPath, "//a"},
		{"x=//span", StrategyXPath, "//span"},
		{"text=Login", StrategyXPath, "text()"},
		{"text:Search", StrategyXPath, "contains"},
		{"text^Start", StrategyXPath, "starts-with"},
		{"text$End", StrategyXPath, "substring"},
		{"@href=/api", StrategyXPath, "@href"},
		{"@@class=btn@@type=submit", StrategyXPath, "and"},
		{"@|class=btn@@type=submit", StrategyXPath, "or"},
		{"@!disabled", StrategyXPath, "not"},
		{"tag:button@type=submit", StrategyXPath, "//button"},
		{"plain text", StrategyXPath, "contains"},
		{"", StrategyCSS, "*"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			res := Resolve(tc.input)
			if res.Strategy != tc.strategy {
				t.Errorf("Resolve(%q).Strategy = %d, want %d", tc.input, res.Strategy, tc.strategy)
			}
			if tc.contains != "" && !containsStr(res.Value, tc.contains) {
				t.Errorf("Resolve(%q).Value = %q, want it to contain %q", tc.input, res.Value, tc.contains)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestXPathQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", `"it's"`},
		{`say "hi"`, `'say "hi"'`},
	}
	for _, tc := range tests {
		got := xpathQuote(tc.input)
		if got != tc.want {
			t.Errorf("xpathQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDispatchUnknownAction(t *testing.T) {
	bt := New()
	in := &Input{Action: "nonexistent_action_xyz"}
	result := bt.dispatch(context.Background(), in)
	if !containsSubstr(result, "Unknown action") {
		t.Errorf("dispatch unknown action returned %q, want 'Unknown action' prefix", result)
	}
}

func TestGetActivityDescription(t *testing.T) {
	bt := New()
	tests := []struct {
		input json.RawMessage
		want  string
	}{
		{json.RawMessage(`{"action":"navigate","url":"https://example.com"}`), "Navigating to https://example.com"},
		{json.RawMessage(`{"action":"screenshot"}`), "Taking screenshot"},
		{json.RawMessage(`{"action":"smart_click","locator":"#btn"}`), "Clicking #btn"},
		{json.RawMessage(`{"action":"list_sessions"}`), "Browser: list_sessions"},
	}
	for _, tc := range tests {
		got := bt.GetActivityDescription(tc.input)
		if got != tc.want {
			t.Errorf("GetActivityDescription(%s) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNetworkListenerInit(t *testing.T) {
	// Can't fully test without a real page, but ensure constructor works
	nl := NewNetworkListener(nil)
	if nl == nil {
		t.Fatal("NewNetworkListener returned nil")
	}
	if nl.maxPackets != 500 {
		t.Errorf("maxPackets = %d, want 500", nl.maxPackets)
	}
	if nl.IsActive() {
		t.Error("new listener should not be active")
	}
	if nl.Count() != 0 {
		t.Error("new listener should have 0 packets")
	}
}

func TestSessionManagerInit(t *testing.T) {
	// Verify the global manager is accessible
	mgr := getManager()
	if mgr == nil {
		t.Fatal("getManager() returned nil")
	}
	list := mgr.ListSessions()
	if len(list) != 0 {
		t.Errorf("fresh manager has %d sessions, want 0", len(list))
	}
}
