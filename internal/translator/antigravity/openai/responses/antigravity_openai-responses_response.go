package responses

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"

	. "github.com/router-for-me/CLIProxyAPI/v6/internal/translator/gemini/openai/responses"
	"github.com/tidwall/gjson"
)

func ConvertAntigravityResponseToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	responseResult := gjson.GetBytes(rawJSON, "response")
	if responseResult.Exists() {
		rawJSON = []byte(responseResult.Raw)
	}
	events := ConvertGeminiResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
	if session := publicapi.FromContext(ctx); session != nil {
		return session.RestoreSSE(events)
	}
	return events
}

func ConvertAntigravityResponseToOpenAIResponsesNonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) string {
	responseResult := gjson.GetBytes(rawJSON, "response")
	if responseResult.Exists() {
		rawJSON = []byte(responseResult.Raw)
	}

	requestResult := gjson.GetBytes(originalRequestRawJSON, "request")
	if responseResult.Exists() {
		originalRequestRawJSON = []byte(requestResult.Raw)
	}

	requestResult = gjson.GetBytes(requestRawJSON, "request")
	if responseResult.Exists() {
		requestRawJSON = []byte(requestResult.Raw)
	}

	body := ConvertGeminiResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
	if session := publicapi.FromContext(ctx); session != nil {
		return session.RestoreJSON(body)
	}
	return body
}
