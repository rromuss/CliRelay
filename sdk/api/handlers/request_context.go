package handlers

import (
	"context"
	"strings"
)

func pinnedAuthIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(pinnedAuthContextKey{})
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}

func selectedAuthIDCallbackFromContext(ctx context.Context) func(string) {
	if ctx == nil {
		return nil
	}
	raw := ctx.Value(selectedAuthCallbackContextKey{})
	if callback, ok := raw.(func(string)); ok && callback != nil {
		return callback
	}
	return nil
}

func executionSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(executionSessionContextKey{})
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}
