package publicapi

import (
	"encoding/base64"
	"errors"
	"strings"
)

// validateInlineRaster never fetches a URL or opens a path. The caller's JSON
// body limit also bounds the total encoded data across replayed image history.
func validateInlineRaster(value string, limit int) error {
	header, encoded, ok := strings.Cut(value, ",")
	mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if !ok || header != "data:"+mime+";base64" || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp") {
		return errors.New("only inline PNG, JPEG and WebP images are supported")
	}
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return errors.New("inline image is empty or too large")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > limit || base64.StdEncoding.EncodeToString(data) != encoded || !validRasterSignature(data, mime) {
		return errors.New("invalid inline raster image")
	}
	return nil
}

func validateContentParts(value any, toolOutput bool, role string) error {
	parts, ok := value.([]any)
	if !ok {
		if toolOutput {
			if _, ok := value.(string); !ok {
				return errors.New("tool output must be text or a content array")
			}
		}
		return nil
	}
	for _, value := range parts {
		part, ok := value.(map[string]any)
		if !ok {
			return errors.New("invalid content part")
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "input_text", "output_text", "":
			if _, ok := part["text"].(string); !ok {
				return errors.New("content text must be a string")
			}
		case "input_image":
			if !toolOutput && role != "user" && role != "" {
				return errors.New("inline images require a user message or tool result")
			}
			url, ok := part["image_url"].(string)
			if !ok {
				return errors.New("inline image_url is required")
			}
			if err := validateInlineRaster(url, int(BodyLimit)); err != nil {
				return err
			}
		default:
			return errors.New("unsupported content part type")
		}
	}
	return nil
}
