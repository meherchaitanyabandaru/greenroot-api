package auditlog

import "context"

type requestIDKey struct{}
type nurseryIDKey struct{}

// WithRequestID stores the request correlation ID in the context.
// Called once per request by the audit middleware.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext retrieves the correlation ID set by the middleware.
func RequestIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey{}).(string)
	return s
}

// WithNurseryID stores the actor's nursery in the context so audit writes
// downstream don't need it passed explicitly.
func WithNurseryID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, nurseryIDKey{}, id)
}

// NurseryIDFromContext retrieves the nursery ID stored by the middleware.
func NurseryIDFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(nurseryIDKey{}).(int64)
	return id
}
