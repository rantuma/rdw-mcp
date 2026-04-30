package rdw

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidKenteken is returned by ValidateKenteken when the input does not
// match any of the 14 official Dutch license plate sidecodes.
var ErrInvalidKenteken = errors.New("invalid kenteken")

// kentekenSidecodes lists the 14 official RDW sidecodes (post-1951) as
// regular expressions matching the cleaned (no separators, uppercase) form.
//
// Reference: https://nl.wikipedia.org/wiki/Nederlands_kenteken
//
//nolint:gochecknoglobals // immutable lookup table
var kentekenSidecodes = []*regexp.Regexp{
	regexp.MustCompile(`^[A-Z]{2}[0-9]{4}$`),         // 1.  XX-99-99
	regexp.MustCompile(`^[0-9]{4}[A-Z]{2}$`),         // 2.  99-99-XX
	regexp.MustCompile(`^[0-9]{2}[A-Z]{2}[0-9]{2}$`), // 3.  99-XX-99
	regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z]{2}$`), // 4.  XX-99-XX
	regexp.MustCompile(`^[A-Z]{4}[0-9]{2}$`),         // 5.  XX-XX-99
	regexp.MustCompile(`^[0-9]{2}[A-Z]{4}$`),         // 6.  99-XX-XX
	regexp.MustCompile(`^[0-9]{2}[A-Z]{3}[0-9]$`),    // 7.  99-XXX-9
	regexp.MustCompile(`^[0-9][A-Z]{3}[0-9]{2}$`),    // 8.  9-XXX-99
	regexp.MustCompile(`^[A-Z]{2}[0-9]{3}[A-Z]$`),    // 9.  XX-999-X
	regexp.MustCompile(`^[A-Z][0-9]{3}[A-Z]{2}$`),    // 10. X-999-XX
	regexp.MustCompile(`^[A-Z]{3}[0-9]{2}[A-Z]$`),    // 11. XXX-99-X
	regexp.MustCompile(`^[A-Z][0-9]{2}[A-Z]{3}$`),    // 12. X-99-XXX
	regexp.MustCompile(`^[0-9][A-Z]{2}[0-9]{3}$`),    // 13. 9-XX-999
	regexp.MustCompile(`^[0-9]{3}[A-Z]{2}[0-9]$`),    // 14. 999-XX-9
}

// CleanKenteken strips hyphens / spaces and uppercases the input.
func CleanKenteken(raw string) string {
	cleaned := strings.ReplaceAll(raw, "-", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	return strings.ToUpper(cleaned)
}

// ValidateKenteken normalises raw and returns the cleaned form when it matches
// one of the 14 official Dutch sidecodes. It returns ErrInvalidKenteken (wrapped
// with diagnostic context) otherwise.
func ValidateKenteken(raw string) (string, error) {
	cleaned := CleanKenteken(raw)
	if cleaned == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidKenteken)
	}

	for _, re := range kentekenSidecodes {
		if re.MatchString(cleaned) {
			return cleaned, nil
		}
	}

	return "", fmt.Errorf(
		"%w: %q does not match any of the 14 official sidecodes",
		ErrInvalidKenteken,
		cleaned,
	)
}
