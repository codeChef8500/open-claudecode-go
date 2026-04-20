package engine

import (
	"encoding/json"
	"testing"
)

func TestEmptyUsage(t *testing.T) {
	u := EmptyUsage()
	if u.InputTokens != 0 {
		t.Errorf("input_tokens = %d, want 0", u.InputTokens)
	}
	if u.ServiceTier != "standard" {
		t.Errorf("service_tier = %s, want standard", u.ServiceTier)
	}
	if u.Speed != "standard" {
		t.Errorf("speed = %s, want standard", u.Speed)
	}
	if u.Iterations == nil {
		t.Error("iterations should be non-nil empty slice")
	}
}

func TestUpdateUsage_Nil(t *testing.T) {
	u := EmptyUsage()
	u.InputTokens = 100
	result := UpdateUsage(u, nil)
	if result.InputTokens != 100 {
		t.Errorf("input_tokens = %d, want 100", result.InputTokens)
	}
}

func TestUpdateUsage_MessageStart(t *testing.T) {
	u := EmptyUsage()
	input := 500
	output := 0
	cacheRead := 200
	part := &PartialUsage{
		InputTokens:          &input,
		OutputTokens:         &output,
		CacheReadInputTokens: &cacheRead,
	}
	result := UpdateUsage(u, part)
	if result.InputTokens != 500 {
		t.Errorf("input_tokens = %d, want 500", result.InputTokens)
	}
	// output_tokens uses null-coalesce: even 0 overwrites
	if result.OutputTokens != 0 {
		t.Errorf("output_tokens = %d, want 0", result.OutputTokens)
	}
	if result.CacheReadInputTokens != 200 {
		t.Errorf("cache_read = %d, want 200", result.CacheReadInputTokens)
	}
}

func TestUpdateUsage_ZeroInputNoOverwrite(t *testing.T) {
	u := EmptyUsage()
	u.InputTokens = 500
	zero := 0
	part := &PartialUsage{InputTokens: &zero}
	result := UpdateUsage(u, part)
	// Zero input_tokens should NOT overwrite (> 0 guard)
	if result.InputTokens != 500 {
		t.Errorf("input_tokens = %d, want 500 (zero should not overwrite)", result.InputTokens)
	}
}

func TestUpdateUsage_MessageDelta(t *testing.T) {
	u := EmptyUsage()
	u.InputTokens = 500
	u.OutputTokens = 10
	output := 150
	part := &PartialUsage{
		OutputTokens: &output,
		ServerToolUse: &ServerToolUse{
			WebSearchRequests: 2,
		},
	}
	result := UpdateUsage(u, part)
	if result.InputTokens != 500 {
		t.Errorf("input_tokens = %d, want 500 (not in delta)", result.InputTokens)
	}
	if result.OutputTokens != 150 {
		t.Errorf("output_tokens = %d, want 150", result.OutputTokens)
	}
	if result.ServerToolUse.WebSearchRequests != 2 {
		t.Errorf("web_search = %d, want 2", result.ServerToolUse.WebSearchRequests)
	}
}

func TestUpdateUsage_CacheCreation(t *testing.T) {
	u := EmptyUsage()
	part := &PartialUsage{
		CacheCreation: &CacheCreation{
			Ephemeral1hInputTokens: 100,
			Ephemeral5mInputTokens: 50,
		},
	}
	result := UpdateUsage(u, part)
	if result.CacheCreation.Ephemeral1hInputTokens != 100 {
		t.Errorf("ephemeral_1h = %d, want 100", result.CacheCreation.Ephemeral1hInputTokens)
	}
	if result.CacheCreation.Ephemeral5mInputTokens != 50 {
		t.Errorf("ephemeral_5m = %d, want 50", result.CacheCreation.Ephemeral5mInputTokens)
	}
}

func TestUpdateUsage_Speed(t *testing.T) {
	u := EmptyUsage()
	part := &PartialUsage{Speed: "fast"}
	result := UpdateUsage(u, part)
	if result.Speed != "fast" {
		t.Errorf("speed = %s, want fast", result.Speed)
	}
}

func TestAccumulateUsage_Basic(t *testing.T) {
	total := EmptyUsage()
	total.InputTokens = 100
	total.OutputTokens = 50
	total.ServerToolUse.WebSearchRequests = 1

	msg := EmptyUsage()
	msg.InputTokens = 200
	msg.OutputTokens = 80
	msg.ServerToolUse.WebSearchRequests = 3
	msg.ServiceTier = "priority"

	result := AccumulateUsage(total, msg)
	if result.InputTokens != 300 {
		t.Errorf("input_tokens = %d, want 300", result.InputTokens)
	}
	if result.OutputTokens != 130 {
		t.Errorf("output_tokens = %d, want 130", result.OutputTokens)
	}
	if result.ServerToolUse.WebSearchRequests != 4 {
		t.Errorf("web_search = %d, want 4", result.ServerToolUse.WebSearchRequests)
	}
	// service_tier should be from message (most recent)
	if result.ServiceTier != "priority" {
		t.Errorf("service_tier = %s, want priority", result.ServiceTier)
	}
}

func TestAccumulateUsage_CacheCreation(t *testing.T) {
	total := EmptyUsage()
	total.CacheCreation.Ephemeral1hInputTokens = 10

	msg := EmptyUsage()
	msg.CacheCreation.Ephemeral1hInputTokens = 20
	msg.CacheCreation.Ephemeral5mInputTokens = 5

	result := AccumulateUsage(total, msg)
	if result.CacheCreation.Ephemeral1hInputTokens != 30 {
		t.Errorf("ephemeral_1h = %d, want 30", result.CacheCreation.Ephemeral1hInputTokens)
	}
	if result.CacheCreation.Ephemeral5mInputTokens != 5 {
		t.Errorf("ephemeral_5m = %d, want 5", result.CacheCreation.Ephemeral5mInputTokens)
	}
}

func TestAccumulateUsage_CacheDeleted(t *testing.T) {
	total := EmptyUsage()
	total.CacheDeletedInputTokens = 100

	msg := EmptyUsage()
	msg.CacheDeletedInputTokens = 50

	result := AccumulateUsage(total, msg)
	if result.CacheDeletedInputTokens != 150 {
		t.Errorf("cache_deleted = %d, want 150", result.CacheDeletedInputTokens)
	}
}

func TestAccumulateUsage_MostRecent(t *testing.T) {
	total := EmptyUsage()
	total.InferenceGeo = "us-east-1"
	total.Speed = "standard"

	msg := EmptyUsage()
	msg.InferenceGeo = "eu-west-1"
	msg.Speed = "fast"
	msg.Iterations = []Iteration{{UsageOutputTokens: 100}}

	result := AccumulateUsage(total, msg)
	if result.InferenceGeo != "eu-west-1" {
		t.Errorf("inference_geo = %s, want eu-west-1", result.InferenceGeo)
	}
	if result.Speed != "fast" {
		t.Errorf("speed = %s, want fast", result.Speed)
	}
	if len(result.Iterations) != 1 {
		t.Errorf("iterations len = %d, want 1", len(result.Iterations))
	}
}

func TestNonNullableUsage_JSON(t *testing.T) {
	u := EmptyUsage()
	u.InputTokens = 100
	u.OutputTokens = 50
	u.ServerToolUse.WebSearchRequests = 2

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var decoded NonNullableUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.InputTokens != 100 {
		t.Errorf("input_tokens = %d, want 100", decoded.InputTokens)
	}
	if decoded.ServerToolUse.WebSearchRequests != 2 {
		t.Errorf("web_search = %d, want 2", decoded.ServerToolUse.WebSearchRequests)
	}
}

func TestUsageToNonNullable(t *testing.T) {
	u := &UsageStats{
		InputTokens:              100,
		OutputTokens:             50,
		CacheCreationInputTokens: 10,
		CacheReadInputTokens:     20,
		CacheDeletedInputTokens:  5,
	}
	nnu := UsageToNonNullable(u)
	if nnu.InputTokens != 100 {
		t.Errorf("input_tokens = %d, want 100", nnu.InputTokens)
	}
	if nnu.CacheDeletedInputTokens != 5 {
		t.Errorf("cache_deleted = %d, want 5", nnu.CacheDeletedInputTokens)
	}
	if nnu.ServiceTier != "standard" {
		t.Errorf("service_tier = %s, want standard", nnu.ServiceTier)
	}
}

func TestUsageToNonNullable_Nil(t *testing.T) {
	nnu := UsageToNonNullable(nil)
	if nnu.InputTokens != 0 {
		t.Errorf("input_tokens = %d, want 0", nnu.InputTokens)
	}
}

func TestNonNullableToUsageStats(t *testing.T) {
	nnu := EmptyUsage()
	nnu.InputTokens = 200
	nnu.OutputTokens = 100
	nnu.CacheDeletedInputTokens = 30

	us := NonNullableToUsageStats(nnu)
	if us.InputTokens != 200 {
		t.Errorf("input_tokens = %d, want 200", us.InputTokens)
	}
	if us.CacheDeletedInputTokens != 30 {
		t.Errorf("cache_deleted = %d, want 30", us.CacheDeletedInputTokens)
	}
}
