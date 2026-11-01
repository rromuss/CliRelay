package publicapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestImageRequestIsHermesCompatibleAndCannotBecomeGenericChat(t *testing.T) {
	body, err := ImageRequestToChat([]byte(`{"prompt":"a sliced strawberry","aspect_ratio":"16:9","n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(body, &result) != nil || result["model"] != ImageModel || result["stream"] != false {
		t.Fatal("image operation changed", string(body))
	}
	if cfg := result["image_config"].(map[string]any); cfg["image_size"] != "1K" || cfg["aspect_ratio"] != "16:9" {
		t.Fatal("image size/aspect not bounded")
	}
	for _, raw := range []string{
		`{}`, `null`, `{"prompt":"  "}`, `{"prompt":"x","n":2}`, `{"prompt":"x","n":0}`,
		`{"prompt":"x","response_format":"url"}`, `{"prompt":"x","aspect_ratio":"1000:1"}`,
		`{"prompt":"x","model":"gpt-image-2"}`, `{"prompt":"x","messages":[]}`,
		`{"prompt":"x","image_url":"http://127.0.0.1/private"}`, `{"prompt":"x","stream":true}`,
		`{"prompt":"x"} {"prompt":"y"}`, `{"prompt":"` + strings.Repeat("x", 16001) + `"}`,
	} {
		if _, err := ImageRequestToChat([]byte(raw)); err == nil {
			t.Fatalf("accepted forbidden request: %.100s", raw)
		}
	}
}

func TestImageResponseOnlyReturnsBoundedInlineRaster(t *testing.T) {
	var fixture bytes.Buffer
	if err := png.Encode(&fixture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(fixture.Bytes())
	wrap := func(value string) []byte {
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"images": []any{map[string]any{"image_url": map[string]any{"url": value}}}}}}})
		return out
	}
	body, err := ImageResponseFromChat(wrap("data:image/png;base64,"+encoded), 123)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Created int64 `json:"created"`
		Data    []struct {
			Base64 string `json:"b64_json"`
			MIME   string `json:"mime_type"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.Created != 123 || len(result.Data) != 1 || result.Data[0].Base64 != encoded || result.Data[0].MIME != "image/png" {
		t.Fatal("valid image not preserved")
	}
	for _, value := range []string{"https://example.com/image.png", "http://127.0.0.1/private", "data:image/svg+xml;base64," + encoded, "data:image/png;base64,aGVsbG8=", "data:image/png;base64,***"} {
		if _, err := ImageResponseFromChat(wrap(value), 123); err == nil {
			t.Fatal("unsafe image response accepted")
		}
	}
	if _, err := ImageResponseFromChat([]byte(`{"choices":[]}`), 123); err == nil {
		t.Fatal("empty provider response accepted")
	}
	large := strings.Repeat("A", (ImageOutputLimit/3+1)*4)
	if _, err := ImageResponseFromChat(wrap("data:image/png;base64,"+large), 123); err == nil {
		t.Fatal("oversized response accepted")
	}
}
