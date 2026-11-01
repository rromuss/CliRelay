package api

import (
	"bytes"
	"context"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestExternalRoutesFailClosedAndPreserveHermes(t *testing.T) {
	if err := usage.InitDB(filepath.Join(t.TempDir(), "usage.db"), config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(usage.CloseDB)
	private := usage.APIKeyRow{Key: "hermes-fixture", Name: "Hermes"}
	public := usage.APIKeyRow{Key: "laptop-fixture", Name: "Laptop", ExternalAccess: true, AllowedModels: []string{publicapi.Model}, AllowedChannels: []string{"fixture-channel"}, DailyLimit: 300, RPMLimit: 10, ConcurrencyLimit: 2}
	for _, r := range []usage.APIKeyRow{private, public} {
		if err := usage.UpsertAPIKey(r); err != nil {
			t.Fatal(err)
		}
	}
	s := newTestServer(t)
	for _, tc := range []struct {
		method, path, key, body string
		status                  int
	}{
		{"GET", "/v1/models", private.Key, "", 200},
		{"GET", "/external/v1/models", private.Key, "", 403},
		{"GET", "/external/v1/models", public.Key, "", 200},
		{"GET", "/external/v1/models", "invalid", "", 401},
		{"GET", "/external/v1/models?key=laptop-fixture", public.Key, "", 400},
		{"GET", "/external/v1/ws", public.Key, "", 404},
		{"GET", "/external/v1/responses", public.Key, "", 404},
		{"POST", "/external/v0/management/config", public.Key, "{}", 404},
		{"POST", "/external/v1/chat/completions", public.Key, "{}", 404},
		{"GET", "/external/v1/%6dodels", public.Key, "", 404},
		{"POST", "/external/v1/responses", public.Key, `{"model":"mh/gpt-6-luna"}`, 403},
		{"POST", "/external/v1/responses", public.Key, strings.Repeat("x", int(publicapi.BodyLimit)+1), 413},
		{"GET", "/external/codex/models?client_version=0.166.0", public.Key, "", 200},
		{"GET", "/external/codex/models?client_version=unknown", public.Key, "", 400},
	} {
		t.Run(tc.method+tc.path+tc.key, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.key)
			w := httptest.NewRecorder()
			s.engine.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %.200s", w.Code, tc.status, w.Body.String())
			}
		})
	}
	entry := usage.GetAPIKey(public.Key)
	entry.ExternalAccess = false
	if err := usage.UpdateAPIKeyByID(*entry); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/external/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+public.Key)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatal("external flag change not effective", w.Code)
	}
}

func TestExternalConcurrencyIsIndependentOfPrivateTraffic(t *testing.T) {
	a := &externalAdmission{active: map[string]int{}}
	if !a.acquire("laptop", 2) || !a.acquire("laptop", 2) || a.acquire("other", 2) {
		t.Fatal("global bound not enforced")
	}
	a.release("laptop")
	if !a.acquire("other", 1) || a.acquire("other", 1) {
		t.Fatal("per-key concurrency not enforced")
	}
	a.release("laptop")
	a.release("other")
	if a.total != 0 || len(a.active) != 0 {
		t.Fatal("slots leaked")
	}
}

func TestExternalHTTPStreamingCancellationAndPoolIsolation(t *testing.T) {
	if err := usage.InitDB(filepath.Join(t.TempDir(), "usage.db"), config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(usage.CloseDB)
	for _, r := range []usage.APIKeyRow{
		{Key: "private-pool-fixture", Name: "Hermes"},
		{Key: "public-pool-fixture", ExternalAccess: true, AllowedModels: []string{publicapi.Model}, AllowedChannels: []string{"fixture-channel"}, DailyLimit: 300, RPMLimit: 100, ConcurrencyLimit: 2},
	} {
		if err := usage.UpsertAPIKey(r); err != nil {
			t.Fatal(err)
		}
	}
	original := newTestServer(t)
	engine := gin.New()
	engine.Use(externalBoundary())
	s := &Server{engine: engine, accessManager: original.accessManager}
	finished := make(chan struct{}, 2)
	var block atomic.Bool
	block.Store(true)
	s.setupExternalRoutes(func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(200, "event: response.created\ndata: {}\n\n")
		c.Writer.Flush()
		if block.Load() {
			<-c.Request.Context().Done()
			finished <- struct{}{}
		}
	})
	engine.GET("/v1/models", AuthMiddleware(original.accessManager), func(c *gin.Context) { c.JSON(200, gin.H{"private": "unchanged"}) })
	server := httptest.NewServer(engine)
	defer server.Close()
	request := func(ctx context.Context, path, key string) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+path, strings.NewReader(`{"model":"gemini-3.8-flash-tiered","stream":true,"input":"fixture"}`))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	first := request(ctx1, "/external/v1/responses", "public-pool-fixture")
	defer first.Body.Close()
	second := request(ctx2, "/external/v1/responses", "public-pool-fixture")
	defer second.Body.Close()
	if first.StatusCode != 200 || second.StatusCode != 200 {
		t.Fatal("stream did not start incrementally")
	}
	third := request(context.Background(), "/external/v1/responses", "public-pool-fixture")
	third.Body.Close()
	if third.StatusCode != 429 {
		t.Fatal("external pool exceeded")
	}
	req, _ := http.NewRequest("GET", server.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer private-pool-fixture")
	private, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, private.Body)
	private.Body.Close()
	if private.StatusCode != 200 {
		t.Fatal("external pool blocked private Hermes")
	}
	cancel1()
	cancel2()
	for i := 0; i < 2; i++ {
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("client cancellation did not reach handler")
		}
	}
	block.Store(false)
	fourth := request(context.Background(), "/external/v1/responses", "public-pool-fixture")
	fourth.Body.Close()
	if fourth.StatusCode != 200 {
		t.Fatal("cancelled stream leaked a slot")
	}
}
