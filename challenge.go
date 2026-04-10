package zlib

import (
	"crypto/sha1"
	"fmt"
	"regexp"
	"strconv"
)

// challengeRe matches the 40-char hex challenge string embedded in the JS.
// The page contains: ['<HEX40>','c_token=','array']
var challengeRe = regexp.MustCompile(`'([0-9A-Fa-f]{40})','c_token=`)

// solveChallenge parses the JS challenge from html and returns the c_token
// value by brute-forcing the counter until the SHA1 proof-of-work is met.
//
// Algorithm (reverse-engineered from site JS):
//
//	c   = 40-char hex string from page
//	n1  = int(c[0], base 16)          // e.g. '6' → 6
//	find i=0,1,2,... such that:
//	  sha1(c + strconv.Itoa(i))[n1]   == 0xb0
//	  sha1(c + strconv.Itoa(i))[n1+1] == 0x0b
//	c_token = c + strconv.Itoa(i)
func solveChallenge(html string) (string, error) {
	m := challengeRe.FindStringSubmatch(html)
	if m == nil {
		return "", fmt.Errorf("challenge string not found in page")
	}
	c := m[1]

	n1, err := strconv.ParseInt(string(c[0]), 16, 32)
	if err != nil {
		return "", fmt.Errorf("bad challenge prefix: %w", err)
	}
	idx := int(n1)

	for i := 0; i < 10_000_000; i++ {
		input := c + strconv.Itoa(i)
		h := sha1.Sum([]byte(input))
		if h[idx] == 0xb0 && h[idx+1] == 0x0b {
			return input, nil
		}
	}
	return "", fmt.Errorf("challenge: no solution found within 10M iterations")
}
