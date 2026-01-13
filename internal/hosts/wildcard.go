package hosts

import (
	"strings"
)

// wildcardMatch checks if name matches the wildcard pattern.
// Pattern "*.example.com." matches "foo.example.com." but not "foo.bar.example.com."
func wildcardMatch(pattern, name string) bool {
	pattern = strings.ToLower(pattern)
	name = strings.ToLower(name)

	if pattern == name {
		return true
	}

	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	suffix := pattern[1:] // ".example.com."

	if !strings.HasSuffix(name, suffix) {
		return false
	}

	prefix := name[:len(name)-len(suffix)]
	return !strings.Contains(prefix, ".")
}

// selectBestMatch selects the best matching pattern for a hostname.
// Priority: exact match > longest wildcard > shorter wildcard
func selectBestMatch(patterns []string, name string) string {
	name = strings.ToLower(name)
	var bestMatch string
	bestScore := -1

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		if !wildcardMatch(pattern, name) {
			continue
		}

		score := matchScore(pattern, name)
		if score > bestScore {
			bestScore = score
			bestMatch = pattern
		}
	}

	return bestMatch
}

// matchScore returns a score for pattern match quality.
func matchScore(pattern, name string) int {
	if pattern == name {
		return 1 << 30
	}
	return len(pattern)
}

// isWildcard checks if pattern is a wildcard pattern.
func isWildcard(pattern string) bool {
	return strings.HasPrefix(pattern, "*.")
}
