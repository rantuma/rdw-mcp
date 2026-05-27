package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewSampler checks that OTEL_TRACES_SAMPLER selects the expected sampler.
// We assert on Sampler.Description() rather than reaching into the SDK types,
// since that is the stable, public way the SDK identifies a sampler.
func TestNewSampler(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		arg     string
		wantSub string
	}{
		{"default when unset", "", "", "ParentBased{root:AlwaysOnSampler"},
		{"parentbased_always_on", "parentbased_always_on", "", "ParentBased{root:AlwaysOnSampler"},
		{"always_on", "always_on", "", "AlwaysOnSampler"},
		{"always_off", "always_off", "", "AlwaysOffSampler"},
		{"traceidratio", "traceidratio", "0.25", "TraceIDRatioBased{0.25}"},
		{
			"parentbased_always_off",
			"parentbased_always_off",
			"",
			"ParentBased{root:AlwaysOffSampler",
		},
		{
			"parentbased_traceidratio",
			"parentbased_traceidratio",
			"0.1",
			"ParentBased{root:TraceIDRatioBased{0.1}",
		},
		{"unknown falls back to default", "bogus", "", "ParentBased{root:AlwaysOnSampler"},
		{"ratio defaults to 1 when arg unset", "traceidratio", "", "TraceIDRatioBased{1}"},
		{"ratio defaults to 1 when arg invalid", "traceidratio", "nope", "TraceIDRatioBased{1}"},
		{"ratio clamps below zero", "traceidratio", "-5", "TraceIDRatioBased{0}"},
		{"ratio clamps above one", "traceidratio", "5", "TraceIDRatioBased{1}"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tc.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.arg)

			assert.Contains(t, newSampler().Description(), tc.wantSub)
		})
	}
}
