package publicapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const ImageModel = "gemini-3.1-flash-image"
const ImageTimeoutSeconds = 300
const ImageOutputLimit = 20 << 20

// ImageRequestToChat exposes only the narrow generation operation used by the
// Hermes adapter. References are inline raster data, never remote URLs.
func ImageRequestToChat(raw []byte) ([]byte, error) {
	if int64(len(raw)) > BodyLimit {
		return nil, errors.New("image request is too large")
	}
	var request struct {
		Model           string   `json:"model"`
		Prompt          string   `json:"prompt"`
		AspectRatio     string   `json:"aspect_ratio"`
		N               *int     `json:"n"`
		ResponseFormat  string   `json:"response_format"`
		ReferenceImages []string `json:"reference_images"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return nil, errors.New("invalid image generation request")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("invalid image generation request")
	}
	if request.Model != "" && request.Model != ImageModel {
		return nil, errors.New("model is not available on the external image API")
	}
	if strings.TrimSpace(request.Prompt) == "" || len(request.Prompt) > 16000 {
		return nil, errors.New("image prompt must contain 1 to 16000 UTF-8 bytes")
	}
	if request.N != nil && *request.N != 1 {
		return nil, errors.New("only one image per request is supported")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "b64_json" {
		return nil, errors.New("only b64_json image responses are supported")
	}
	if request.AspectRatio == "" {
		request.AspectRatio = "1:1"
	}
	switch request.AspectRatio {
	case "1:1", "16:9", "9:16":
	default:
		return nil, errors.New("unsupported image aspect ratio")
	}
	if len(request.ReferenceImages) > 14 {
		return nil, errors.New("at most 14 reference images are supported")
	}
	content := []any{map[string]any{"type": "text", "text": request.Prompt}}
	for _, image := range request.ReferenceImages {
		if err := validateInlineRaster(image, int(BodyLimit)); err != nil {
			return nil, err
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": image}})
	}
	return json.Marshal(map[string]any{
		"model": ImageModel, "modalities": []string{"image", "text"}, "stream": false,
		"messages":     []any{map[string]any{"role": "user", "content": content}},
		"image_config": map[string]any{"aspect_ratio": request.AspectRatio, "image_size": "1K"},
	})
}

// ImageResponseFromChat accepts inline raster data only. No caller or upstream
// URL is fetched, and no provider messages or credential-bearing errors escape.
func ImageResponseFromChat(raw []byte, created int64) ([]byte, error) {
	if len(raw) > ImageOutputLimit*2 {
		return nil, errors.New("image response is too large")
	}
	var response struct {
		Choices []struct {
			Message struct {
				Images []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Choices) == 0 || len(response.Choices[0].Message.Images) == 0 {
		return nil, errors.New("image provider returned no image")
	}
	value := response.Choices[0].Message.Images[0].ImageURL.URL
	header, encoded, ok := strings.Cut(value, ",")
	mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if !ok || header != "data:"+mime+";base64" || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp") {
		return nil, errors.New("image provider returned unsupported image data")
	}
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(ImageOutputLimit) {
		return nil, errors.New("image response is too large or empty")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > ImageOutputLimit || !validRasterSignature(data, mime) {
		return nil, errors.New("image provider returned invalid image data")
	}
	return json.Marshal(map[string]any{"created": created, "data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(data), "mime_type": mime}}})
}

func validRasterSignature(data []byte, mime string) bool {
	switch mime {
	case "image/png":
		return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})
	case "image/webp":
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	}
	return false
}
