package api

import (
	"context"
	_ "embed"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/middleware"
	keysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
)

type externalAdmission struct {
	mu     sync.Mutex
	active map[string]int
	total  int
}

func (a *externalAdmission) acquire(id string, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.total >= 2 || a.active[id] >= limit {
		return false
	}
	a.active[id]++
	a.total++
	return true
}
func (a *externalAdmission) release(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.total--
	a.active[id]--
	if a.active[id] == 0 {
		delete(a.active, id)
	}
}

// Installed before request capture/decompression: even rejected external input
// never enters the private detailed logging path or grouped-route fallback.
func externalBoundary() gin.HandlerFunc {
	var rateMu sync.Mutex
	var window time.Time
	var requests int
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, publicapi.Prefix) && path != "/external" {
			c.Next()
			return
		}
		c.Request = c.Request.WithContext(publicapi.WithSession(c.Request.Context(), publicapi.NewSession()))
		c.Header("Cache-Control", "no-store")
		valid := (c.Request.Method == http.MethodGet && (path == "/external/v1/models" || path == "/external/codex/models")) || (c.Request.Method == http.MethodPost && (path == "/external/v1/responses" || path == "/external/v1/images/generations"))
		if !valid || c.Request.URL.EscapedPath() != path {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		for key := range c.Request.URL.Query() {
			if key != "client_version" {
				c.AbortWithStatusJSON(400, gin.H{"error": "unsupported query parameter"})
				return
			}
		}
		if c.GetHeader("Upgrade") != "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		rateMu.Lock()
		now := time.Now()
		if window.IsZero() || now.Sub(window) >= time.Minute {
			window = now
			requests = 0
		}
		requests++
		overLimit := requests > 60
		rateMu.Unlock()
		if overLimit {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(429, gin.H{"error": "external request rate exceeded"})
			return
		}
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")) == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "bearer API key required"})
			return
		}
		for _, h := range []string{"Cookie", "X-Management-Key", "X-Api-Key", "X-Goog-Api-Key", "X-Hermes-Fallback-Policy"} {
			c.Request.Header.Del(h)
		}
		deadline := 120 * time.Second
		if path == "/external/v1/images/generations" {
			deadline = publicapi.ImageTimeoutSeconds * time.Second
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), deadline)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, publicapi.BodyLimit)
		c.Next()
	}
}

func (s *Server) setupExternalRoutes(respond gin.HandlerFunc) {
	admission := &externalAdmission{active: map[string]int{}}
	settings := keysettings.NewService(nil)
	group := s.engine.Group("/external")
	group.Use(AuthMiddleware(s.accessManager))
	group.Use(func(c *gin.Context) {
		entry := settings.ExternalEntry(c.GetString("apiKey"))
		if entry == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid API key"})
			return
		}
		if !entry.ExternalAccess {
			c.AbortWithStatusJSON(403, gin.H{"error": "external access is disabled"})
			return
		}
		if len(externalAllowedModels(entry)) == 0 || len(entry.AllowedChannels) == 0 || entry.DailyLimit <= 0 || entry.RPMLimit <= 0 || entry.ConcurrencyLimit <= 0 {
			c.AbortWithStatusJSON(403, gin.H{"error": "external access requires an explicit model, channel and positive limits"})
			return
		}
		metadata := map[string]string{"allowed-channels": strings.Join(entry.AllowedChannels, ","), "allowed-channel-groups": strings.Join(entry.AllowedChannelGroups, ",")}
		metadata["daily-limit"] = strconv.Itoa(entry.DailyLimit)
		metadata["total-quota"] = strconv.Itoa(entry.TotalQuota)
		metadata["rpm-limit"] = strconv.Itoa(entry.RPMLimit)
		metadata["tpm-limit"] = strconv.Itoa(entry.TPMLimit)
		metadata["spending-limit"] = strconv.FormatFloat(entry.SpendingLimit, 'f', -1, 64)
		c.Set("accessMetadata", metadata)
		c.Set("externalEntry", entry)
		c.Next()
	})
	group.GET("/v1/models", func(c *gin.Context) {
		models := []gin.H{}
		for _, model := range externalAllowedModels(settings.ExternalEntry(c.GetString("apiKey"))) {
			models = append(models, gin.H{"id": model, "object": "model", "owned_by": "antigravity"})
		}
		c.JSON(200, gin.H{"object": "list", "data": models})
	})
	group.GET("/codex/models", func(c *gin.Context) {
		if !externalModelAllowed(settings.ExternalEntry(c.GetString("apiKey")), publicapi.Model) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		externalCodexCatalog(c)
	})
	group.POST("/v1/responses", middleware.QuotaMiddleware(), externalInferenceHandler(settings, admission, publicapi.Model, func(c *gin.Context, body []byte) ([]byte, error) {
		return publicapi.FromContext(c.Request.Context()).Normalize(body)
	}, respond))
	group.POST("/v1/images/generations", middleware.QuotaMiddleware(), externalInferenceHandler(settings, admission, publicapi.ImageModel, func(_ *gin.Context, body []byte) ([]byte, error) {
		return publicapi.ImageRequestToChat(body)
	}, s.externalImageGeneration))
}

// Versioned Codex metadata is embedded rather than fetched with caller credentials.
// The fixture is generated from the qualified client's bundled schema.
//
//go:embed external_catalog.json
var externalCatalog []byte

func externalCodexCatalog(c *gin.Context) {
	if len(externalCatalog) == 0 {
		c.AbortWithStatusJSON(503, gin.H{"error": "Codex catalog is not qualified"})
		return
	}
	version := c.Query("client_version")
	if version != "0.166.0" {
		c.AbortWithStatusJSON(400, gin.H{"error": "unsupported Codex client version; qualified version: 0.166.0"})
		return
	}
	c.Data(200, "application/json", externalCatalog)
}
