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
// Length alone is not a discriminator: /forgot-password-reset is exactly 21
// URL-safe characters. Neither is "contains a digit and an uppercase letter":
// /oauth2CallbackHandler is 21 characters and satisfies both, and normalising
// a static route to /:id merges it with unrelated endpoints and corrupts their
// request counts and latencies. See looksLikeGeneratedToken for the test that
// separates them.
var pathSegmentToken = regexp.MustCompile(`^[A-Za-z0-9_-]{21,22}$`)

// charClass groups a byte of the URL-safe alphabet: lowercase, uppercase,
// digit, or the two symbols. The segment is ASCII by construction, so the
// callers can index it bytewise.
func charClass(b byte) int {
	switch {
	case b >= 'a' && b <= 'z':
		return 0
	case b >= 'A' && b <= 'Z':
		return 1
	case b >= '0' && b <= '9':
		return 2
	default:
		return 3
	}
}

// looksLikeGeneratedToken reports whether seg has the shape of a generated
// URL-safe identifier rather than a route name.
//
// The discriminator is how OFTEN the character class changes. A name is built
// from words, and a word is a run of one class: /oauth2CallbackHandler changes
// class 5 times in 21 characters, /s3BucketAccessPolicy1 8 times. A token
// drawn uniformly from a 64-symbol alphabet changes on roughly two thirds of
// adjacent pairs -- about 13 times -- because nothing keeps a class going.
// Requiring at least HALF the pairs to cross a class boundary is a gap no
// readable name closes: it would need words averaging two characters.
//
// A digit and an uppercase letter are still required, which is what excludes
// hyphenated lowercase names like /forgot-password-reset cheaply.
//
// This matches ~90% of random tokens. The residue is the deliberate side to
// miss on: an unmatched token costs one extra series, while a false positive
// silently merges real routes and corrupts the series they already have.
func looksLikeGeneratedToken(seg string) bool {
	if !pathSegmentToken.MatchString(seg) {
		return false
	}

	var hasDigit, hasUpper bool
	transitions, previous := 0, -1
	for i := 0; i < len(seg); i++ {
		class := charClass(seg[i])
		switch class {
		case 1:
			hasUpper = true
		case 2:
			hasDigit = true
		}
		if i > 0 && class != previous {
			transitions++
		}
		previous = class
	}
	if !hasDigit || !hasUpper {
		return false
	}

	// At least half of the len(seg)-1 adjacent pairs cross a class boundary.
	return transitions*2 >= len(seg)-1
}

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
		if pathSegmentID.MatchString(seg) || looksLikeGeneratedToken(seg) {
			segments[i] = ":id"
		}
	}
	return "/" + strings.Join(segments, "/")
}
