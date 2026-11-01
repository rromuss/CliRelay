package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type externalImageExecutor struct {
	calls    int
	payload  []byte
	metadata map[string]any
	channel  string
}

func (e *externalImageExecutor) Identifier() string { return "antigravity" }
func (e *externalImageExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.payload, e.metadata, e.channel = req.Payload, opts.Metadata, auth.Label
	if req.Model != publicapi.ImageModel || opts.Stream || opts.Alt != "" || !publicapi.IsExternal(ctx) {
		return coreexecutor.Response{}, errors.New("incorrect image execution boundary")
	}
	image := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfixture"))
	return coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,` + image + `"}}]}}]}`)}, nil
}
func (e *externalImageExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("streaming forbidden")
}
func (e *externalImageExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}
func (e *externalImageExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("unsupported")
}
func (e *externalImageExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("unsupported")
}

func TestExternalImageOperationAuthorizationAndDurableAdmission(t *testing.T) {
	if err := usage.InitDB(filepath.Join(t.TempDir(), "usage.db"), config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(usage.CloseDB)
	for _, entry := range []usage.APIKeyRow{
		{Key: "image-private-fixture"},
		{Key: "image-chat-only-fixture", ExternalAccess: true, AllowedModels: []string{publicapi.Model}, AllowedChannels: []string{"image-team"}, DailyLimit: 1, RPMLimit: 100, ConcurrencyLimit: 2},
		{Key: "image-allowed-fixture", ExternalAccess: true, AllowedModels: []string{publicapi.ImageModel}, AllowedChannels: []string{"image-team"}, DailyLimit: 1, RPMLimit: 100, ConcurrencyLimit: 2},
		{Key: "image-and-chat-fixture", ExternalAccess: true, AllowedModels: []string{publicapi.Model, publicapi.ImageModel}, AllowedChannels: []string{"image-team"}, DailyLimit: 1, RPMLimit: 100, ConcurrencyLimit: 2},
	} {
		if err := usage.UpsertAPIKey(entry); err != nil {
			t.Fatal(err)
		}
	}
	s := newTestServer(t)
	executor := &externalImageExecutor{}
	s.handlers.AuthManager.RegisterExecutor(executor)
	for _, channel := range []string{"image-team", "other-team"} {
		id := "external-image-" + channel
		_, err := s.handlers.AuthManager.Register(context.Background(), &coreauth.Auth{ID: id, Provider: "antigravity", Label: channel, Status: coreauth.StatusActive})
		if err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "antigravity", []*registry.ModelInfo{{ID: publicapi.ImageModel}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	request := func(path, method, key, body string, expected int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, r)
		if w.Code != expected {
			t.Fatalf("%s got %d want %d: %.250s", path, w.Code, expected, w.Body.String())
		}
		return w.Body.String()
	}
	path := "/external/v1/images/generations"
	request(path, "POST", "image-private-fixture", `{"prompt":"fixture"}`, 403)
	request(path, "POST", "image-chat-only-fixture", `{"prompt":"fixture"}`, 403)
	request(path, "GET", "image-allowed-fixture", "", 404)
	request("/external/v1/images/edits", "POST", "image-allowed-fixture", `{}`, 404)
	request(path, "POST", "image-allowed-fixture", `{"prompt":"fixture","model":"gpt-image-2"}`, 403)
	request(path, "POST", "image-allowed-fixture", `{"prompt":"fixture","image_url":"http://127.0.0.1/private"}`, 400)
	request("/external/codex/models?client_version=0.166.0", "GET", "image-allowed-fixture", "", 403)
	models := request("/external/v1/models", "GET", "image-allowed-fixture", "", 200)
	if !strings.Contains(models, publicapi.ImageModel) || strings.Contains(models, publicapi.Model) {
		t.Fatal("model catalog exceeds key scope")
	}
	response := request(path, "POST", "image-allowed-fixture", `{"prompt":"fixture","aspect_ratio":"9:16"}`, 200)
	var output map[string]any
	if json.Unmarshal([]byte(response), &output) != nil || output["data"] == nil || strings.Contains(response, "choices") {
		t.Fatal("image envelope conversion failed")
	}
	if executor.calls != 1 || executor.channel != "image-team" || executor.metadata["external_api"] != true || executor.metadata[coreexecutor.SinglePickMetadataKey] != true {
		t.Fatal("image operation lost channel/single-attempt isolation")
	}
	request(path, "POST", "image-allowed-fixture", `{"prompt":"fixture"}`, 429)
	if executor.calls != 1 {
		t.Fatal("daily quota reached upstream again")
	}
	request(path, "POST", "image-and-chat-fixture", `{"prompt":"fixture"}`, 200)
	request("/external/v1/responses", "POST", "image-and-chat-fixture", `{"model":"gemini-3.8-flash-tiered","input":"fixture"}`, 429)
	if executor.calls != 2 {
		t.Fatal("image and text do not share the stable-key quota")
	}
	// Profile/permission edits must take effect without replacing the old key
	// or depending on the auth provider's initial in-memory model snapshot.
	entry := usage.GetAPIKey("image-chat-only-fixture")
	entry.AllowedModels = []string{publicapi.Model, publicapi.ImageModel}
	if err := usage.UpdateAPIKeyByID(*entry); err != nil {
		t.Fatal(err)
	}
	request(path, "POST", entry.Key, `{"prompt":"fixture"}`, 200)
	entry.AllowedModels = []string{publicapi.Model}
	if err := usage.UpdateAPIKeyByID(*entry); err != nil {
		t.Fatal(err)
	}
	request(path, "POST", entry.Key, `{"prompt":"fixture"}`, 403)
	request("/v1/models", "GET", "image-private-fixture", "", 200)
}
