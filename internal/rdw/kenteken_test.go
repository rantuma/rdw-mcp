package rdw_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

func TestValidateKenteken(t *testing.T) {
	t.Parallel()

	valid := []string{
		"AB1234",   // sidecode 1: XX-99-99
		"1234AB",   // sidecode 2: 99-99-XX
		"12AB34",   // sidecode 3
		"AB12CD",   // sidecode 4
		"ABCD12",   // sidecode 5
		"12ABCD",   // sidecode 6
		"12ABC3",   // sidecode 7
		"1ABC23",   // sidecode 8
		"AB123C",   // sidecode 9
		"A123BC",   // sidecode 10
		"ABC12D",   // sidecode 11
		"A12BCD",   // sidecode 12
		"1AB234",   // sidecode 13
		"123AB4",   // sidecode 14
		"ab-12-cd", // hyphenated lowercase
	}

	for _, plate := range valid {
		t.Run("valid_"+plate, func(t *testing.T) {
			t.Parallel()

			cleaned, err := rdw.ValidateKenteken(plate)
			require.NoError(t, err)
			assert.NotEmpty(t, cleaned)
			assert.Equal(t, rdw.CleanKenteken(plate), cleaned)
		})
	}

	invalid := []string{
		"",        // empty
		"---",     // separators only
		"ABCDEF",  // letters only
		"123456",  // digits only
		"AB12C",   // too short
		"AB12CDE", // too long
	}

	for _, plate := range invalid {
		t.Run("invalid_"+plate, func(t *testing.T) {
			t.Parallel()

			_, err := rdw.ValidateKenteken(plate)
			require.Error(t, err)
			assert.ErrorIs(t, err, rdw.ErrInvalidKenteken)
		})
	}
}

func TestCleanKenteken_Internal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "AB12CD", rdw.CleanKenteken("ab-12-cd"))
	assert.Empty(t, rdw.CleanKenteken(""))
	assert.Empty(t, rdw.CleanKenteken(" - - "))
}
