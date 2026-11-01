package api

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers/openai"
)

func (s *Server) externalImageGeneration(c *gin.Context) {
	if s.handlers == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "image execution unavailable"})
		return
	}
	payload, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image request"})
		return
	}
	handler := openai.NewOpenAIAPIHandler(s.handlers)
	ctx, cancel := handler.GetContextWithCancel(handler, c, c.Request.Context())
	defer cancel()
	// The ingress guard has already produced the same fixed Chat Completions
	// payload as the working Hermes image adapter. Execution stays in-process.
	body, _, err := handler.ExecuteWithAuthManager(ctx, handler.HandlerType(), publicapi.ImageModel, payload, "")
	if err != nil {
		status := err.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": "image provider request failed"})
		return
	}
	result, parseErr := publicapi.ImageResponseFromChat(body, time.Now().Unix())
	if parseErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "image provider returned no valid inline image"})
		return
	}
	c.Data(http.StatusOK, "application/json", result)
}
