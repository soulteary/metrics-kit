package metrics

import (
	"regexp"
	"strings"
)

// Default max length for a single label value to avoid unbounded cardinality and exposition bloat.
// Prometheus does not enforce a limit; this is a reasonable default for safety.
const DefaultLabelValueMaxLength = 256

// SanitizeLabelValue returns a value safe for use as a Prometheus label value.
// It replaces newlines and carriage returns (which would break the exposition format),
// escapes backslashes, and truncates to maxLen runes. Use 0 for maxLen to skip truncation.
// Call this for any label value that may come from untrusted input (e.g. request path, user-provided strings).
func SanitizeLabelValue(s string, maxLen int) string {
	if s == "" {
		return s
	}
	// Replace characters that break the Prometheus text format or could inject
	// extra lines. A label value is written as name="value", so the quote
	// matters as much as the backslash and the newline did.
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "\"", "_")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if maxLen <= 0 {
		return s
	}
	// Truncate by rune count
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// pathSegmentID matches a single path segment that looks like a numeric ID or UUID.
// Used by DefaultPathNormalize to reduce label cardinality.
var pathSegmentID = regexp.MustCompile(`^(` +
	`\d+` + // numeric ids
	`|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` + // UUID
	`|[0-9a-fA-F]{16,}` + // hex ids: 16 covers a 64-bit id, which the previous 24 floor missed
	`|[0-9A-HJKMNP-TV-Z]{26}` + // ULID / Crockford base32
	`|[A-Za-z0-9_-]{21,22}` + // nanoid, and base64url-encoded 16-byte tokens
	`)$`)

// DefaultPathNormalize normalizes request paths for use as metric labels to avoid cardinality explosion.
// It replaces numeric and UUID-like path segments with ":id", e.g. /users/123 -> /users/:id,
// /items/550e8400-e29b-41d4-a716-446655440000 -> /items/:id.
// Use this as PathTransformFunc in production so each distinct ID does not create a new time series.
func DefaultPathNormalize(path string) string {
	if path == "" || path == "/" {
		return path
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range segments {
		if pathSegmentID.MatchString(seg) {
			segments[i] = ":id"
		}
	}
	return "/" + strings.Join(segments, "/")
}
