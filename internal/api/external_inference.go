package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	keysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	sqlapikey "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/apikey"
)

func externalModelAllowed(entry *config.APIKeyEntry, model string) bool {
	if entry == nil {
		return false
	}
	for _, allowed := range entry.AllowedModels {
		if model == allowed {
			return true
		}
	}
	return false
}

func externalAllowedModels(entry *config.APIKeyEntry) []string {
	models := []string{}
	for _, model := range []string{publicapi.Model, publicapi.ImageModel} {
		if externalModelAllowed(entry, model) {
			models = append(models, model)
		}
	}
	return models
}

// Both public operations share stable-key admission and the two-slot external
// pool. Native private Hermes routes do not enter this handler.
func externalInferenceHandler(settings *keysettings.Service, admission *externalAdmission, model string, normalize func(*gin.Context, []byte) ([]byte, error), respond gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		entry := settings.ExternalEntry(c.GetString("apiKey"))
		if entry == nil || !entry.ExternalAccess || !externalModelAllowed(entry, model) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if !admission.acquire(entry.ID, entry.ConcurrencyLimit) {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(429, gin.H{"error": "external concurrency limit exceeded"})
			return
		}
		defer admission.release(entry.ID)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.AbortWithStatusJSON(413, gin.H{"error": "request body exceeds 8 MiB"})
			} else {
				c.AbortWithStatusJSON(400, gin.H{"error": "invalid request body"})
			}
			return
		}
		body, err = normalize(c, body)
		if err != nil {
			status := 400
			if strings.Contains(err.Error(), "model is not") {
				status = 403
			}
			c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
			return
		}
		if int64(len(body)) > publicapi.BodyLimit {
			c.AbortWithStatusJSON(413, gin.H{"error": "normalized request exceeds 8 MiB"})
			return
		}
		if err = settings.AdmitExternal(c.Request.Context(), *entry, time.Now()); err != nil {
			status, message := 503, "external admission unavailable"
			if errors.Is(err, sqlapikey.ErrExternalDailyLimit) || errors.Is(err, sqlapikey.ErrExternalRateLimit) {
				status, message = 429, err.Error()
				c.Header("Retry-After", "60")
			}
			c.AbortWithStatusJSON(status, gin.H{"error": message})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		respond(c)
	}
}
