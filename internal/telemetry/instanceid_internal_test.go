package telemetry

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// uuidV4Pattern is the canonical 8-4-4-4-12 hex layout with the version-4 nibble
// and RFC 4122 variant bits.
var uuidV4Pattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestInstanceID(t *testing.T) {
	id := instanceID()
	assert.Len(t, id, 36, "service.instance.id should be a 36-char UUID")
	assert.Regexp(t, uuidV4Pattern, id, "should be a v4 UUID")
	assert.NotEqual(t, id, instanceID(), "successive ids should differ")
}
