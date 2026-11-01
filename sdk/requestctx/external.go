package requestctx

import "context"

type externalIngressKey struct{}

// This marker is set by the trusted HTTP boundary, never from a client header.
func WithExternalIngress(ctx context.Context) context.Context {
	return context.WithValue(ctx, externalIngressKey{}, true)
}
func IsExternalIngress(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(externalIngressKey{}).(bool)
	return value
}
