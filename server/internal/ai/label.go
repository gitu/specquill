package ai

import "context"

// A label names what an AI call is FOR, so the server log can say which phase
// spent the time ("extract survey ~regulations") instead of listing
// indistinguishable calls. Carried on the context so no call signature has to
// change, and purely diagnostic — nothing behaves differently with or without.
type labelKey struct{}

// WithLabel tags every AI call made with the returned context.
func WithLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, labelKey{}, label)
}

// labelOf renders the tag for a log line (" [label]", or "" when untagged).
func labelOf(ctx context.Context) string {
	if s, ok := ctx.Value(labelKey{}).(string); ok && s != "" {
		return " [" + s + "]"
	}
	return ""
}
