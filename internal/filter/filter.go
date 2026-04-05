// Package filter matches audio files against keyword expressions using path data.
package filter

import (
	"strings"
)

// Filter returns the subset of files where at least one keyword is a
// case-insensitive substring of the file's absolute path.
func Filter(files []string, keywords []string) []string {
	lower := lowerAll(keywords)

	var matched []string
	for _, path := range files {
		if matches(path, lower) {
			matched = append(matched, path)
		}
	}
	return matched
}

// lowerAll returns a new slice with every string lowercased.
func lowerAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}

// matches reports whether any lowercased keyword is a substring of the file's
// lowercased absolute path.
func matches(path string, lowerKeywords []string) bool {
	lowerPath := strings.ToLower(path)
	for _, kw := range lowerKeywords {
		if strings.Contains(lowerPath, kw) {
			return true
		}
	}
	return false
}
