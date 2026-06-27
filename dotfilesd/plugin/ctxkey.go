package plugin

import "context"

// ctxKey is used to store the Context in the Go context.
type ctxKey struct{}

// WithContext stores a plugin Context in the Go context.
func WithContext(ctx context.Context, pc Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, pc)
}

// ExtractContext extracts the plugin Context from a Go context.
// Custom RPC handlers can call this to access the plugin's daemon APIs.
// Returns nil if no plugin Context is present.
func ExtractContext(ctx context.Context) Context {
	pc, _ := ctx.Value(ctxKey{}).(Context)
	return pc
}
