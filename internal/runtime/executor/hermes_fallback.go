package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"net/http"
	"strings"
)

func hermesFallbackEffort(opts cliproxyexecutor.Options) string {
	v2, _ := opts.Metadata[cliproxyexecutor.HermesFallbackV2MetadataKey].(bool)
	if v2 {
		return gjson.GetBytes(opts.OriginalRequest, "reasoning_effort").String()
	}
	return "medium" // Preserve the established v1 contract.
}

const retiredGeminiFlashNotice = "Gemini 3.5 Flash is no longer available. Please switch to Gemini 3.7 Flash in the latest version of Antigravity."

func hermesAntigravityRetiredError(model string, body []byte, opts cliproxyexecutor.Options) error {
	if !hermesFallbackEnabled(opts) && !hermesManualEnabled(opts) {
		return nil
	}
	// Exact retired family only: Flash-Lite remains a separate, active model.
	switch strings.TrimPrefix(model, "ag/") {
	case "gemini-3.5-flash", "gemini-3.5-flash-low", "gemini-3.5-flash-extra-low", "gemini-3.5-flash-medium", "gemini-3.5-flash-high", "gemini-3.5-flash-tiered":
	default:
		return nil
	}
	root := gjson.ParseBytes(body)
	if wrapped := root.Get("response"); wrapped.IsObject() {
		root = wrapped
	}
	if root.Get("modelVersion").String() != "" || root.Get("promptFeedback.blockReason").String() != "" {
		return nil
	}
	for _, counter := range root.Get("usageMetadata").Map() {
		if !counter.IsArray() && !counter.IsObject() && counter.Int() != 0 {
			return nil
		}
	}
	candidates := root.Get("candidates").Array()
	if len(candidates) != 1 {
		return nil
	}
	candidate := candidates[0]
	if reason := candidate.Get("finishReason").String(); reason != "" && reason != "STOP" {
		return nil
	}
	parts := candidate.Get("content.parts").Array()
	if len(parts) == 0 {
		return nil
	}
	var texts []string
	for _, part := range parts {
		if !part.Get("text").Exists() || part.Get("thought").Bool() {
			return nil
		}
		for key := range part.Map() {
			if key != "text" && key != "thought" {
				return nil
			}
		}
		texts = append(texts, part.Get("text").String())
	}
	if strings.Join(strings.Fields(strings.Join(texts, " ")), " ") != retiredGeminiFlashNotice {
		return nil
	}
	return statusErr{code: http.StatusGone, msg: `{"error":{"type":"model_unavailable","code":"model_retired","message":"This Google model is retired. Choose an available model."}}`}
}

func hermesFallbackEnabled(opts cliproxyexecutor.Options) bool {
	enabled, _ := opts.Metadata[cliproxyexecutor.HermesFallbackMetadataKey].(bool)
	return enabled
}

func hermesManualEnabled(opts cliproxyexecutor.Options) bool {
	enabled, _ := opts.Metadata[cliproxyexecutor.HermesManualMetadataKey].(bool)
	return enabled
}

func hermesManualEffort(opts cliproxyexecutor.Options) string {
	return gjson.GetBytes(opts.OriginalRequest, "reasoning_effort").String()
}

func antigravityAttemptPlan(auth *cliproxyauth.Auth, cfg *config.Config, opts cliproxyexecutor.Options, urls []string) (int, []string) {
	if hermesFallbackEnabled(opts) || hermesManualEnabled(opts) {
		if len(urls) > 1 {
			urls = urls[:1]
		}
		return 1, urls
	}
	return antigravityRetryAttempts(auth, cfg), urls
}

// New Antigravity route IDs are dynamically registered without Thinking metadata
// in the pinned proxy. Preserve exactly the requested medium after payload rules,
// instead of stripping it or silently choosing another effort.
func applyHermesGeminiMedium(body []byte) ([]byte, error) {
	return applyHermesGeminiEffort(body, "medium")
}

func applyHermesGeminiEffort(body []byte, effort string) ([]byte, error) {
	var err error
	for _, path := range []string{
		"request.generationConfig.thinkingConfig.thinkingBudget",
		"request.generationConfig.thinkingConfig.thinking_budget",
		"request.generationConfig.thinkingConfig.thinking_level",
	} {
		body, err = sjson.DeleteBytes(body, path)
		if err != nil {
			return nil, err
		}
	}
	if effort == "" || effort == "none" {
		return sjson.DeleteBytes(body, "request.generationConfig.thinkingConfig")
	}
	return sjson.SetBytes(body, "request.generationConfig.thinkingConfig.thinkingLevel", effort)
}

func applyHermesOpenAIMedium(body []byte) ([]byte, error) {
	return applyHermesOpenAIEffort(body, "medium")
}

func applyHermesOpenAIEffort(body []byte, effort string) ([]byte, error) {
	var err error
	body, err = sjson.DeleteBytes(body, "reasoning")
	if err != nil {
		return nil, err
	}
	if effort == "" || effort == "none" {
		return sjson.DeleteBytes(body, "reasoning_effort")
	}
	return sjson.SetBytes(body, "reasoning_effort", effort)
}
