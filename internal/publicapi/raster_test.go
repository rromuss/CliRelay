package publicapi

import (
	"encoding/json"
	"strings"
	"testing"
)

const rasterFixture = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aGWkAAAAASUVORK5CYII="

func TestReplayedImagesAndToolResultsArePreserved(t *testing.T) {
	parts := []any{map[string]any{"type": "input_text", "text": "browser docs\n" + strings.Repeat("state ", 12000)}, map[string]any{"type": "input_image", "image_url": rasterFixture}, map[string]any{"type": "input_text", "text": "after screenshot"}}
	raw, _ := json.Marshal(map[string]any{"model": Model, "tools": []any{map[string]any{"type": "namespace", "name": "mcp__cua_repl", "tools": []any{map[string]any{"type": "function", "name": "js"}}}}, "input": []any{
		map[string]any{"role": "user", "content": parts},
		map[string]any{"type": "function_call", "namespace": "mcp__cua_repl", "name": "js", "call_id": "browser-1", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "browser-1", "output": parts},
	}})
	for i := 0; i < 2; i++ {
		out, err := NewSession().Normalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		json.Unmarshal(out, &result)
		items := result["input"].([]any)
		for _, index := range []int{0, 2} {
			key := "content"
			if index == 2 {
				key = "output"
			}
			actual, _ := json.Marshal(items[index].(map[string]any)[key])
			expected, _ := json.Marshal(parts)
			if string(actual) != string(expected) {
				t.Fatal("replayed multimodal content lost or reordered")
			}
		}
	}
}

func TestExternalImagesRejectURLsPathsCorruptionAndUnsupportedContent(t *testing.T) {
	for _, value := range []string{"https://example.com/image.png", "file:///private/photo.png", "/private/photo.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/jpeg;base64,aGVsbG8=", "data:image/png;base64,***", rasterFixture + "\n"} {
		for _, tool := range []bool{false, true} {
			part := map[string]any{"type": "input_image", "image_url": value}
			item := map[string]any{"role": "user", "content": []any{part}}
			if tool {
				item = map[string]any{"type": "function_call_output", "call_id": "x", "output": []any{part}}
			}
			raw, _ := json.Marshal(map[string]any{"model": Model, "input": []any{item}})
			if _, err := NewSession().Normalize(raw); err == nil {
				t.Fatal("unsafe image accepted")
			}
		}
	}
	for _, output := range []any{map[string]any{"text": "silently lost"}, []any{map[string]any{"type": "input_audio", "data": "x"}}, []any{map[string]any{"type": "input_text", "text": 42}}} {
		raw, _ := json.Marshal(map[string]any{"model": Model, "input": []any{map[string]any{"type": "function_call_output", "call_id": "x", "output": output}}})
		if _, err := NewSession().Normalize(raw); err == nil {
			t.Fatal("unsupported tool output accepted")
		}
	}
	if _, err := NewSession().Normalize([]byte(strings.Repeat(" ", int(BodyLimit)+1))); err == nil {
		t.Fatal("body limit lost")
	}
}

func TestImageEditsKeepHermesPayloadAndBoundReferences(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"prompt": "change the background", "reference_images": []string{rasterFixture, rasterFixture}})
	out, err := ImageRequestToChat(raw)
	if err != nil {
		t.Fatal(err)
	}
	var chat struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(out, &chat)
	if len(chat.Messages) != 1 || len(chat.Messages[0].Content) != 3 || chat.Messages[0].Content[1].ImageURL.URL != rasterFixture || chat.Messages[0].Content[2].ImageURL.URL != rasterFixture {
		t.Fatal("reference order or Hermes image_url payload lost")
	}
	for _, refs := range [][]string{strings.Fields(strings.Repeat(rasterFixture+" ", 15)), {"https://example.com/image.png"}, {"data:image/png;base64,aGVsbG8="}} {
		raw, _ := json.Marshal(map[string]any{"prompt": "edit", "reference_images": refs})
		if _, err := ImageRequestToChat(raw); err == nil {
			t.Fatal("unsafe image edit accepted")
		}
	}
}
