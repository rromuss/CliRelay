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
	delete(meta, "hermes_fallback_requested")
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
