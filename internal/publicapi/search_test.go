package publicapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexDefaultHostedDescriptorUsesOnlyDeclaredClientSearch(t *testing.T) {
	for _, kind := range []string{"web_search", "web_search_preview"} {
		for _, search := range []any{
			map[string]any{"type": "namespace", "name": "mcp__clirelay_search", "tools": []any{map[string]any{"type": "function", "name": "web_search", "parameters": map[string]any{"type": "object"}}}},
			map[string]any{"type": "function", "name": "mcp__clirelay_search__web_search"},
		} {
			raw, _ := json.Marshal(map[string]any{"model": Model, "tools": []any{search, map[string]any{"type": kind}}, "input": "fixture"})
			out, err := NewSession().Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			var normalized struct{ Tools []struct{ Type, Name string } }
			if json.Unmarshal(out, &normalized) != nil || len(normalized.Tools) != 1 || normalized.Tools[0].Type != "function" || !strings.HasPrefix(normalized.Tools[0].Name, "ext_") {
				t.Fatal("only executable client search may reach Gemini")
			}
		}
	}
	for _, tools := range [][]any{
		{map[string]any{"type": "web_search"}},
		{map[string]any{"type": "namespace", "name": "mcp__clirelay_search", "tools": []any{}}, map[string]any{"type": "web_search"}},
		{map[string]any{"type": "function", "name": "other"}, map[string]any{"type": "web_search"}},
	} {
		raw, _ := json.Marshal(map[string]any{"model": Model, "tools": tools})
		if _, err := NewSession().Normalize(raw); err == nil {
			t.Fatal("hosted search silently accepted without a client executor")
		}
	}
}
