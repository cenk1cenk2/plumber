package logger

import (
	"cmp"
	"encoding/base64"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// The placeholder that every registered value and every encoded variant of it is replaced with.
const redactedMask = "[REDACTED]"

/*
Masks the registered values out of the records of the handler.

The values are registered while the application is already running and while the records are already
written out, therefore the replacer that does the masking is rebuilt on every registration and swapped
in atomically, so that a record that is written out concurrently reads a consistent replacer without
waiting for the registration.
*/
type redactor struct {
	// guards the patterns while the replacer is rebuilt
	lock     sync.Mutex
	patterns []string
	replacer atomic.Pointer[strings.Replacer]
}

// Registers the given values together with the encodings of them.
func (r *redactor) add(values ...string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	added := false

	for _, value := range values {
		// an empty pattern matches at every position of a record, which would leave the mask
		// between every single rune of the output
		if value == "" {
			continue
		}

		for _, pattern := range variants(value) {
			if slices.Contains(r.patterns, pattern) {
				continue
			}

			r.patterns = append(r.patterns, pattern)
			added = true
		}
	}

	if !added {
		return
	}

	patterns := slices.Clone(r.patterns)

	// strings.Replacer matches the first pattern that fits at a given position in the order the
	// patterns are handed over, so the longer ones go first to keep a shorter value that overlaps
	// one of them from masking only a part of it.
	slices.SortStableFunc(patterns, func(a, b string) int {
		return cmp.Compare(len(b), len(a))
	})

	pairs := make([]string, 0, len(patterns)*2)

	for _, pattern := range patterns {
		pairs = append(pairs, pattern, redactedMask)
	}

	r.replacer.Store(strings.NewReplacer(pairs...))
}

// Masks every registered value out of the given text, which is a no-op while nothing is registered.
func (r *redactor) redact(text string) string {
	replacer := r.replacer.Load()

	if replacer == nil {
		return text
	}

	return replacer.Replace(text)
}

// Returns the value itself together with the encodings of it that a leak can show up in, where an
// encoding that does not differ from the value or from another encoding of it is left out.
func variants(value string) []string {
	variants := []string{value}

	for _, variant := range []string{
		url.QueryEscape(value),
		base64.StdEncoding.EncodeToString([]byte(value)),
		base64.RawStdEncoding.EncodeToString([]byte(value)),
		base64.URLEncoding.EncodeToString([]byte(value)),
		base64.RawURLEncoding.EncodeToString([]byte(value)),
	} {
		if !slices.Contains(variants, variant) {
			variants = append(variants, variant)
		}
	}

	return variants
}
