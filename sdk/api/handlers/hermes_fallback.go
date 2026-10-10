package handlers

import (
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

// Scope the no-retry contract to explicitly marked non-streaming requests.
// It only reduces execution attempts; authentication/channel access is unchanged.
func configureHermesFallback(meta map[string]any, model string, body []byte) *interfaces.ErrorMessage {
	requested, _ := meta["hermes_fallback_requested"].(bool)
	v2, _ := meta["hermes_fallback_v2_requested"].(bool)
	manual, _ := meta["hermes_manual_requested"].(bool)
	delete(meta, "hermes_fallback_requested")
	delete(meta, "hermes_fallback_v2_requested")
	delete(meta, "hermes_manual_requested")
	if manual {
		if model == "" || gjson.GetBytes(body, "stream").Bool() {
			return &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: fmt.Errorf("Hermes manual mode requires a selected model and non-streaming request")}
		}
		meta[coreexecutor.SinglePickMetadataKey] = true
		meta[coreexecutor.HermesManualMetadataKey] = true
		return nil
	}
	if v2 {
		profiles := map[string]string{
			"gemini-3.8-flash-tiered": "medium",
			"gemini-3.6-flash-medium": "medium",
			"gemini-3.1-flash-lite":   "medium",
			"mh/gpt-6-luna":           "medium",
		}
		effort, ok := profiles[model]
		if !ok || gjson.GetBytes(body, "reasoning_effort").String() != effort || gjson.GetBytes(body, "stream").Bool() {
			return &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: fmt.Errorf("Hermes v2 requires a verified model/effort profile and non-streaming request")}
		}
		meta[coreexecutor.SinglePickMetadataKey] = true
		meta[coreexecutor.HermesFallbackMetadataKey] = true
		meta[coreexecutor.HermesFallbackV2MetadataKey] = true
		return nil
	}
	if !requested {
		return nil
	}
	switch model {
	case "gemini-3.8-flash-tiered", "gemini-3.6-flash-tiered", "gemini-3.5-flash-lite", "gemini-3.1-flash-lite", "mh/gpt-6-luna":
	default:
		return &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: fmt.Errorf("Hermes fallback model is outside the agreed chain")}
	}
	if gjson.GetBytes(body, "reasoning_effort").String() != "medium" || gjson.GetBytes(body, "stream").Bool() {
		return &interfaces.ErrorMessage{StatusCode: http.StatusBadRequest, Error: fmt.Errorf("Hermes fallback requires non-streaming reasoning_effort=medium")}
	}
	meta[coreexecutor.SinglePickMetadataKey] = true
	meta[coreexecutor.HermesFallbackMetadataKey] = true
	return nil
}
