// Package sysprompt implements the system prompt chapter functions, ported
// from claude-code-main/src/constants/prompts.ts.
package sysprompt

// PrependBullets mirrors the TS prependBullets (prompts.ts:L167-173).
//
// Input is a variadic list of items. Each item is either:
//   - a single string  → emitted as " - <item>"
//   - a []string slice → each element emitted as "  - <sub>"  (nested indent)
//
// The returned slice contains the formatted bullet lines in order.
//
// [P1.T4] TS anchor: constants/prompts.ts:L167-173
func PrependBullets(items ...interface{}) []string {
	var out []string
	for _, item := range items {
		switch v := item.(type) {
		case string:
			out = append(out, " - "+v)
		case []string:
			for _, sub := range v {
				out = append(out, "  - "+sub)
			}
		}
	}
	return out
}
