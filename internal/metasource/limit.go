package metasource

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultMaxResults is how many candidates a lookup returns unless told
	// otherwise.
	DefaultMaxResults = 10
	// MaxResultsCeiling is the most a lookup may be asked to return. A source
	// answers at most this many hits per request.
	MaxResultsCeiling = 50

	// maxDetailRequests caps the follow-up requests a source that reads one
	// page per hit (Douban, WeRead) makes for one lookup, so a large limit
	// does not hammer a throttled service.
	maxDetailRequests = 10
)

// ParseMaxResults reads a result limit: a whole number from 1 to
// MaxResultsCeiling. An empty string is DefaultMaxResults.
func ParseMaxResults(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultMaxResults, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxResultsCeiling {
		return 0, fmt.Errorf("enter a whole number from 1 to %d", MaxResultsCeiling)
	}
	return n, nil
}

// resultLimit is limit, or the default when it is unset or out of range.
func resultLimit(limit int) int {
	if limit < 1 || limit > MaxResultsCeiling {
		return DefaultMaxResults
	}
	return limit
}

// fetchCount is how many hits to ask a catalogue for: twice the limit, so that
// dropping poor matches and merging editions of one book still leaves the
// limit, within what the API allows per request.
func fetchCount(limit, apiMax int) int { return min(2*resultLimit(limit), apiMax) }

// detailCount is how many hits a source reads one by one for limit results.
func detailCount(limit int) int { return min(resultLimit(limit), maxDetailRequests) }
