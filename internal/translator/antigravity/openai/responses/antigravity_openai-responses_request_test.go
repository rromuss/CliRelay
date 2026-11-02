package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	gemini "github.com/router-for-me/CLIProxyAPI/v6/internal/translator/gemini/openai/responses"
	"github.com/tidwall/gjson"
)

func TestBrowserDocsAndScreenshotsSurviveFullAntigravityConversion(t *testing.T) {
	image := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aGWkAAAAASUVORK5CYII="
	docs := "Use getAXState and typeText.\n" + strings.Repeat("keep browser documentation\n", 3000)
	parts := []any{map[string]any{"type": "input_text", "text": docs}, map[string]any{"type": "input_image", "image_url": image}, map[string]any{"type": "input_text", "text": "fresh state"}}
	raw, _ := json.Marshal(map[string]any{"model": publicapi.Model, "tools": []any{map[string]any{"type": "namespace", "name": "mcp__cua_repl", "tools": []any{map[string]any{"type": "function", "name": "js"}}}}, "input": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": image}}},
		map[string]any{"type": "function_call", "namespace": "mcp__cua_repl", "name": "js", "call_id": "cua-1", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "cua-1", "output": parts},
		map[string]any{"type": "function_call", "namespace": "mcp__cua_repl", "name": "js", "call_id": "cua-2", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "cua-2", "output": parts},
	}})
	normalized, err := publicapi.NewSession().Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	converted := ConvertOpenAIResponsesRequestToAntigravity(publicapi.Model, normalized, true)
	seen := map[string]bool{}
	contents := gjson.GetBytes(converted, "request.contents")
	if !contents.IsArray() {
		t.Fatal("missing Antigravity contents")
	}
	imageCount := 0
	for _, content := range contents.Array() {
		for _, part := range content.Get("parts").Array() {
			if part.Get("inline_data.data").Exists() || part.Get("inlineData.data").Exists() {
				imageCount++
			}
			fr := part.Get("functionResponse")
			if !fr.Exists() {
				continue
			}
			id := fr.Get("id").String()
			seen[id] = true
			if !strings.HasPrefix(fr.Get("name").String(), "ext_") || fr.Get("response.result.0.text").String() != docs || fr.Get("response.result.2.text").String() != "fresh state" {
				t.Fatal("call alias, docs or order lost")
			}
			blob := fr.Get("parts.0.inlineData")
			if blob.Get("mimeType").String() != "image/png" || blob.Get("data").String() != strings.SplitN(image, ",", 2)[1] || blob.Get("displayName").String() != fr.Get("response.result.1.image_ref.$ref").String() {
				t.Fatal("nested screenshot or reference lost")
			}
		}
	}
	if !seen["cua-1"] || !seen["cua-2"] || imageCount != 1 {
		t.Fatal("repeated call IDs lost or screenshot moved outside tool provenance")
	}
}

func TestLegacyStringToolOutputsKeepTheirMeaning(t *testing.T) {
	for _, value := range []string{"plain\ntext", `{"ok":true}`, "", "null"} {
		raw, _ := json.Marshal(map[string]any{"input": []any{map[string]any{"type": "function_call", "name": "legacy", "call_id": "c", "arguments": "{}"}, map[string]any{"type": "function_call_output", "call_id": "c", "output": value}}})
		out := gemini.ConvertOpenAIResponsesRequestToGemini("fixture", raw, false)
		result := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.response.result")
		if value == "" || value == "null" {
			if result.Exists() {
				t.Fatal("empty legacy result changed")
			}
			continue
		}
		if value == `{"ok":true}` {
			if !result.Get("ok").Bool() {
				t.Fatal("legacy JSON result changed")
			}
		} else if result.String() != value {
			t.Fatal("legacy text result changed")
		}
	}
}
