// Package publicapi owns the opt-in Codex boundary. Private Hermes traffic must
// never inherit its policy or protocol adaptation.
package publicapi

import (
	"context"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/requestctx"
)

const Prefix = "/external/"
const Model = "gemini-3.8-flash-tiered"
const BodyLimit int64 = 8 << 20

type contextKey struct{}

func WithSession(ctx context.Context, session *Session) context.Context {
	return requestctx.WithExternalIngress(context.WithValue(ctx, contextKey{}, session))
}
func FromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(*Session)
	return s
}
func IsExternal(ctx context.Context) bool { return FromContext(ctx) != nil }
