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
	`)$`)

// pathSegmentToken matches a 21- or 22-character URL-safe segment -- the shape
// of a nanoid or a base64url-encoded 16-byte token.
//
// This is a GUESS, and it is only reached through PathNormalizeWithTokens.
// Nothing reliably separates such a token from a route NAME of the same
// length: both draw on the same alphabet, and three successive attempts to
// find a discriminator each fell to an ordinary endpoint.
//
//   - length alone: /forgot-password-reset is exactly 21 URL-safe characters
//   - plus a digit and an uppercase letter: /oauth2CallbackHandler satisfies both
//   - plus a class-transition threshold: /s3ToS3CopyHandlerV2Job crosses it,
//     because acronym-heavy camel case mixes classes as briskly as random text
//
// The two failure modes are not symmetric. Missing a token costs one extra
// series for one route. A false positive silently merges real endpoints into
// /:id, destroying their own request counts and latency histograms and
// polluting whatever they merge into -- and nothing in the metrics says it
// happened. That is why the default no longer guesses.
var pathSegmentToken = regexp.MustCompile(`^[A-Za-z0-9_-]{21,22}$`)

// looksLikeGeneratedToken reports whether seg has the shape of a generated
// URL-safe identifier. Requires a digit and an uppercase letter, which at
// least excludes hyphenated lowercase names like /forgot-password-reset.
//
// Deliberately still a guess -- see pathSegmentToken. It is the caller of
// PathNormalizeWithTokens who knows their own routes and can accept it.
func looksLikeGeneratedToken(seg string) bool {
	if !pathSegmentToken.MatchString(seg) {
		return false
	}

	var hasDigit, hasUpper bool
	for i := 0; i < len(seg); i++ {
		switch {
		case seg[i] >= '0' && seg[i] <= '9':
			hasDigit = true
		case seg[i] >= 'A' && seg[i] <= 'Z':
			hasUpper = true
		}
	}

	return hasDigit && hasUpper
}

// DefaultPathNormalize normalizes request paths for use as metric labels to avoid cardinality explosion.
// It replaces numeric and UUID-like path segments with ":id", e.g. /users/123 -> /users/:id,
// /items/550e8400-e29b-41d4-a716-446655440000 -> /items/:id.
// Use this as PathTransformFunc in production so each distinct ID does not create a new time series.
//
// Only UNAMBIGUOUS id shapes are replaced: all-digit segments, UUIDs, long hex
// strings and ULIDs. None of them can be mistaken for a route name.
//
// Segments that merely look random -- a nanoid, a base64url token -- are left
// alone, because nothing separates one from a route name of the same length
// (see pathSegmentToken). If your routes carry such ids, use
// PathNormalizeWithTokens and check it against your own route table.
func DefaultPathNormalize(path string) string {
	return normalizePath(path, false)
}

// PathNormalizeWithTokens is DefaultPathNormalize plus a guess at nanoid- and
// base64url-shaped segments: 21 or 22 URL-safe characters carrying at least
// one digit and one uppercase letter.
//
// Use it when your paths carry ids of that shape AND no static route of yours
// looks like one -- it cannot tell the difference, and a wrong guess merges a
// real endpoint into /:id, destroying its metrics silently. Check it against
// your route table before turning it on.
func PathNormalizeWithTokens(path string) string {
	return normalizePath(path, true)
}

func normalizePath(path string, guessTokens bool) string {
	if path == "" || path == "/" {
		return path
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range segments {
		if pathSegmentID.MatchString(seg) || (guessTokens && looksLikeGeneratedToken(seg)) {
			segments[i] = ":id"
		}
	}
	return "/" + strings.Join(segments, "/")
}
