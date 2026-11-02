package responses

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Gemini 3 associates screenshots with their real tool result, not a synthetic
// user message. The result array preserves the original text/image ordering;
// each image reference occurs once and points to one nested inlineData part.
func addMultimodalFunctionOutput(response, callID string, output gjson.Result) string {
	result := []any{}
	parts := []any{}
	digest := sha256.Sum256([]byte(callID))
	for i, part := range output.Array() {
		switch part.Get("type").String() {
		case "input_text", "output_text", "":
			result = append(result, map[string]any{"type": "text", "text": part.Get("text").String()})
		case "input_image":
			header, data, ok := strings.Cut(part.Get("image_url").String(), ",")
			if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
				continue
			}
			mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
			extension := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}[mime]
			if extension == "" {
				continue
			}
			name := fmt.Sprintf("tool-%s-%d.%s", hex.EncodeToString(digest[:8]), i, extension)
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "displayName": name, "data": data}})
			result = append(result, map[string]any{"image_ref": map[string]any{"$ref": name}})
		}
	}
	response, _ = sjson.Set(response, "functionResponse.response.result", result)
	if len(parts) > 0 {
		response, _ = sjson.Set(response, "functionResponse.parts", parts)
	}
	return response
}
