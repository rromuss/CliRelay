package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

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
