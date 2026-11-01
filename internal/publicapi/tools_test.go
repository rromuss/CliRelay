package publicapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomNamespaceHistoryAndStreamingRoundTrip(t *testing.T) {
	s := NewSession()
	raw := `{"model":"gemini-3.8-flash-tiered","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"apply_patch","description":"Patch files","format":{"type":"text"}}]}],"input":[{"type":"custom_tool_call","name":"apply_patch","namespace":"functions","call_id":"call-1","input":"line 1\nline 2"},{"type":"custom_tool_call_output","call_id":"call-1","output":"done"}]}`
	normalized, err := s.Normalize([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	json.Unmarshal(normalized, &request)
	tool := request["tools"].([]any)[0].(map[string]any)
	wire := tool["name"].(string)
	input := request["input"].([]any)
	if input[0].(map[string]any)["type"] != "function_call" || input[1].(map[string]any)["call_id"] != "call-1" {
		t.Fatal("history IDs lost")
	}
	item := map[string]any{"type": "function_call", "id": "item-1", "name": wire, "call_id": "call-2", "arguments": "{\"input\":\"hello\\nworld\"}"}
	data, _ := json.Marshal(map[string]any{"type": "response.output_item.added", "item": item})
	got := s.RestoreSSE([]string{"event: response.output_item.added\ndata: " + string(data) + "\n\n"})
	if !strings.Contains(strings.Join(got, ""), `"type":"custom_tool_call"`) || !strings.Contains(strings.Join(got, ""), `"namespace":"functions"`) {
		t.Fatal(got)
	}
	args, _ := json.Marshal(map[string]any{"type": "response.function_call_arguments.done", "item_id": "item-1", "arguments": "{\"input\":\"hello\\nworld\"}", "sequence_number": float64(3)})
	got = s.RestoreSSE([]string{"data: " + string(args) + "\n\n"})
	if len(got) != 2 || !strings.Contains(got[1], `"input":"hello\nworld"`) || !strings.Contains(got[0], "custom_tool_call_input.delta") {
		t.Fatal(got)
	}
	final, _ := json.Marshal(map[string]any{"output": []any{item}})
	if result := s.RestoreJSON(string(final)); !strings.Contains(result, `"name":"apply_patch"`) || !strings.Contains(result, `"call_id":"call-2"`) {
		t.Fatal(result)
	}
}
func TestExternalRejectsUnsupportedRequests(t *testing.T) {
	for _, raw := range []string{`{}`, `{"model":"other"}`, `{"model":"gemini-3.8-flash-tiered","previous_response_id":"old"}`, `{"model":"gemini-3.8-flash-tiered","reasoning":{"effort":"high"}}`, `{"model":"gemini-3.8-flash-tiered","tools":[{"type":"web_search","name":"search"}]}`} {
		if _, err := NewSession().Normalize([]byte(raw)); err == nil {
			t.Fatal("unsupported request silently accepted", raw)
		}
	}
}
