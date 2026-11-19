package metasource

import "strings"

// NormalizeISBN returns s as a valid ISBN-13, or "" when it is not an ISBN.
// Hyphens and spaces are ignored and a valid ISBN-10 is converted, so the
// same book always yields the same key whichever form a source uses.
func NormalizeISBN(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			b = append(b, c)
		case (c == 'x' || c == 'X') && i == len(s)-1:
			b = append(b, 'X')
		case c == '-' || c == ' ':
		case i == 0 && (c == 'i' || c == 'I') && len(s) > 5:
			// An "ISBN" prefix is skipped below.
		default:
			if prefix := strings.ToUpper(strings.TrimSpace(s)); strings.HasPrefix(prefix, "ISBN") {
				return NormalizeISBN(strings.TrimLeft(prefix[4:], ":- "))
			}
			return ""
		}
	}
	digits := string(b)
	if allSame(digits) {
		return ""
	}
	switch len(digits) {
	case 10:
		if !validISBN10(digits) {
			return ""
		}
		core := "978" + digits[:9]
		return core + string(isbn13Check(core))
	case 13:
		if digits[12] == 'X' || isbn13Check(digits[:12]) != digits[12] {
			return ""
		}
		return digits
	}
	return ""
}

func allSame(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return len(s) > 0
}

func validISBN10(s string) bool {
	sum := 0
	for i := 0; i < 10; i++ {
		v := 10
		if s[i] != 'X' {
			if s[i] < '0' || s[i] > '9' {
				return false
			}
			v = int(s[i] - '0')
		} else if i != 9 {
			return false
		}
		sum += v * (10 - i)
	}
	return sum%11 == 0
}

// isbn13Check returns the check digit for the first 12 digits.
func isbn13Check(first12 string) byte {
	sum := 0
	for i := 0; i < 12; i++ {
		d := int(first12[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}
