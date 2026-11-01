package executor

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"net/http/httptest"
	"testing"
)

func TestExternalPrivacyDoesNotChangePrivateCapture(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/external/v1/responses", nil)
	c.Request.Header.Set("Authorization", "Bearer fixture-credential")
	c.Request.Header.Set("Cookie", "fixture-cookie")
	private := context.WithValue(context.Background(), util.ContextKeyGin, c)
	if buildRequestDetailContent(private) == "" {
		t.Fatal("private diagnostics changed")
	}
	ctx := publicapi.WithSession(private, publicapi.NewSession())
	if buildRequestDetailContent(ctx) != "" {
		t.Fatal("external credentials captured")
	}
	for _, model := range []string{publicapi.Model, publicapi.ImageModel} {
		r := newUsageReporter(ctx, "antigravity", model, nil)
		r.setInputContent("fixture-private-prompt")
		r.appendOutputChunk(make([]byte, usageReporterOutputMemoryLimit+100))
		input, output := r.finalizeContent()
		if input != "" || output != "" || r.outputFile != nil {
			t.Fatal("external content retained or spilled", model)
		}
	}
	attempts, urls := antigravityAttemptPlan(nil, nil, coreexecutor.Options{Metadata: map[string]any{"external_api": true}}, []string{"a", "b"})
	if attempts != 1 || len(urls) != 1 {
		t.Fatal("public request can multiply upstream attempts")
	}
}
