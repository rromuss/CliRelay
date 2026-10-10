package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestHermesFallbackRequiresExactModelsAndMedium(t *testing.T) {
	models := []string{"gemini-3.8-flash-tiered", "gemini-3.6-flash-tiered", "gemini-3.5-flash-lite", "gemini-3.1-flash-lite", "mh/gpt-6-luna"}
	for _, model := range models {
		meta := map[string]any{"hermes_fallback_requested": true, "allowed-channels": "synthetic-scope"}
		if err := configureHermesFallback(meta, model, []byte(`{"reasoning_effort":"medium","stream":false}`)); err != nil {
			t.Fatal(err)
		}
		if meta[coreexecutor.SinglePickMetadataKey] != true || meta[coreexecutor.HermesFallbackMetadataKey] != true {
			t.Fatal("retry policy missing")
		}
		if meta["allowed-channels"] != "synthetic-scope" {
			t.Fatal("authorization scope changed")
		}
	}
	for _, tc := range []struct{ model, body string }{
		{"gemini-3.1-pro-high", `{"reasoning_effort":"medium"}`},
		{"mh/gpt-6-luna", `{"reasoning_effort":"high"}`},
		{"mh/gpt-6-luna", `{"reasoning_effort":"medium","stream":true}`},
	} {
		meta := map[string]any{"hermes_fallback_requested": true}
		if configureHermesFallback(meta, tc.model, []byte(tc.body)) == nil {
			t.Fatal("invalid contract accepted", tc)
		}
		if meta[coreexecutor.SinglePickMetadataKey] == true {
			t.Fatal("rejected contract enabled")
		}
	}
}

func TestHermesV2VerifiedProfilesAndLegacyV1Compatibility(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash-tiered", "gemini-3.6-flash-medium", "gemini-3.1-flash-lite", "mh/gpt-6-luna"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("X-Hermes-Fallback-Policy", "v2")
		meta := requestExecutionMetadata(context.WithValue(context.Background(), util.ContextKeyGin, c))
		meta["allowed-channels"] = "synthetic"
		if err := configureHermesFallback(meta, model, []byte(`{"reasoning_effort":"medium"}`)); err != nil {
			t.Fatal(err)
		}
		if meta[coreexecutor.HermesFallbackV2MetadataKey] != true || meta[coreexecutor.SinglePickMetadataKey] != true || meta["allowed-channels"] != "synthetic" {
			t.Fatal("v2 contract missing")
		}
	}
	for _, model := range []string{"gemini-3.5-flash-lite", "gemini-3.7-flash", "gemini-3.1-pro-high"} {
		if configureHermesFallback(map[string]any{"hermes_fallback_v2_requested": true}, model, []byte(`{"reasoning_effort":"medium"}`)) == nil {
			t.Fatal("unverified v2 route accepted", model)
		}
	}
	if configureHermesFallback(map[string]any{"hermes_fallback_v2_requested": true}, "gemini-3.6-flash-medium", []byte(`{"reasoning_effort":"high"}`)) == nil {
		t.Fatal("unverified profile accepted")
	}
	if err := configureHermesFallback(map[string]any{"hermes_fallback_requested": true}, "gemini-3.6-flash-tiered", []byte(`{"reasoning_effort":"medium"}`)); err != nil {
		t.Fatal("legacy v1 broken", err)
	}
	errBody := BuildErrorResponseBody(http.StatusGone, `{"error":{"type":"model_unavailable","code":"model_retired","message":"retired"}}`)
	if !bytes.Contains(errBody, []byte(`"code":"model_retired"`)) {
		t.Fatal("retirement code lost")
	}
}

func TestHermesFallbackAbsentHeaderPreservesNormalRetries(t *testing.T) {
	meta := map[string]any{"allowed-channels": "existing"}
	if err := configureHermesFallback(meta, "other-model", []byte(`{"reasoning_effort":"high"}`)); err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 {
		t.Fatal("ordinary request was changed", meta)
	}
}

func TestHermesFallbackHeaderEntersExecutionMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Hermes-Fallback-Policy", "v1")
	ctx := context.WithValue(context.Background(), util.ContextKeyGin, c)
	meta := requestExecutionMetadata(ctx)
	if meta["hermes_fallback_requested"] != true {
		t.Fatal("missing request marker")
	}
}

func TestHermesManualLocksAnySelectedRouteWithoutForcingMedium(t *testing.T) {
	for _, model := range []string{"gemini-3.1-flash-lite", "mh/gpt-6-sol", "mh-claude/claude-sonnet-5"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("X-Hermes-Fallback-Policy", "manual-v1")
		ctx := context.WithValue(context.Background(), util.ContextKeyGin, c)
		meta := requestExecutionMetadata(ctx)
		meta["allowed-channels"] = "synthetic-scope"
		if err := configureHermesFallback(meta, model, []byte(`{"reasoning_effort":"high","stream":false}`)); err != nil {
			t.Fatal(err)
		}
		if meta[coreexecutor.SinglePickMetadataKey] != true || meta[coreexecutor.HermesManualMetadataKey] != true {
			t.Fatal("manual lock missing", meta)
		}
		if meta[coreexecutor.HermesFallbackMetadataKey] == true || meta["allowed-channels"] != "synthetic-scope" {
			t.Fatal("automatic reasoning or authorization changed")
		}
	}
}

func TestHermesManualRejectsStreamingBeforeAuthOrExecution(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Hermes-Fallback-Policy", "manual-v1")
	ctx := context.WithValue(context.Background(), util.ContextKeyGin, c)
	h := &BaseAPIHandler{} // Any auth/execution access would panic.
	_, _, errs := h.ExecuteStreamWithAuthManager(ctx, "openai", "gemini-3.1-flash-lite", []byte(`{"stream":true}`), "")
	if err := <-errs; err == nil || err.StatusCode != http.StatusBadRequest {
		t.Fatal("manual streaming accepted", err)
	}
	meta := map[string]any{"hermes_manual_requested": true}
	if configureHermesFallback(meta, "gemini-3.1-flash-lite", []byte(`{"stream":true}`)) == nil {
		t.Fatal("invalid manual contract accepted")
	}
}
