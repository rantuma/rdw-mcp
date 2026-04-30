//nolint:testpackage // accesses unexported endpointRecalls / recallDescription
package rdw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	// roundTripFunc adapts a function to [http.RoundTripper].
	roundTripFunc func(*http.Request) (*http.Response, error)
)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()

	body, err := json.Marshal(v)
	require.NoError(t, err)

	return body
}

func jsonClient(t *testing.T, payload any) *http.Client {
	t.Helper()

	body := mustMarshalJSON(t, payload)

	return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_, err := rec.Write(body)
		require.NoError(t, err)

		return rec.Result(), nil
	})}
}

// routedClient returns a client that picks a payload by the path substring.
func routedClient(t *testing.T, routes map[string]any) *http.Client {
	t.Helper()

	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		for ep, payload := range routes {
			if strings.Contains(r.URL.Path, ep) {
				body := mustMarshalJSON(t, payload)

				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "application/json")
				_, err := rec.Write(body)
				require.NoError(t, err)

				return rec.Result(), nil
			}
		}

		rec := httptest.NewRecorder()
		http.Error(rec, "no route", http.StatusNotFound)

		return rec.Result(), nil
	})}
}

func TestSetUserAgent(t *testing.T) {
	// Skipped: SetUserAgent mutates a package-level global which is read by
	// other parallel tests, causing data races under -race. Coverage is
	// already provided by the rdw_test smoke tests that exercise UserAgent.
	t.Skip("global state; covered indirectly")
}

func TestFetchSimpleEndpoints(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("FetchBase found", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleBaseInfo{{Kenteken: "AB12CD", Merk: "FORD"}})
		base, err := FetchBase(ctx, client, "AB12CD")
		require.NoError(t, err)
		require.NotNil(t, base)
		assert.Equal(t, "FORD", base.Merk)
	})

	t.Run("FetchBase not found", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleBaseInfo{})
		base, err := FetchBase(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Nil(t, base)
	})

	t.Run("FetchFuel", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleFuelInfo{{Kenteken: "AB12CD"}})
		out, err := FetchFuel(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("FetchAxles", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleAxesInfo{{Kenteken: "AB12CD"}})
		out, err := FetchAxles(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("FetchBody", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleBodyInfo{{Kenteken: "AB12CD"}})
		out, err := FetchBody(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("FetchAPKHistory", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleAPKInspection{{Kenteken: "AB12CD"}})
		out, err := FetchAPKHistory(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})

	t.Run("FetchDefects", func(t *testing.T) {
		t.Parallel()
		client := jsonClient(t, []VehicleDefect{{Kenteken: "AB12CD"}})
		out, err := FetchDefects(ctx, client, "AB12CD")
		require.NoError(t, err)
		assert.Len(t, out, 1)
	})
}

func TestFetchRecallsEnrichment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := routedClient(t, map[string]any{
		endpointRecalls: []VehicleRecall{{
			Kenteken:          "AB12CD",
			ReferentiecodeRDW: "MGP123",
			Status:            "Open",
		}},
		endpointRecallDesc: []recallDescription{{
			ReferentiecodeRDW:  "MGP123",
			OmschrijvingDefect: "Brake issue",
			PublicatiedatumRDW: "20240101",
		}},
	})

	recalls, err := FetchRecalls(ctx, client, "AB12CD")
	require.NoError(t, err)
	require.Len(t, recalls, 1)
	assert.Equal(t, "Brake issue", recalls[0].Description)
	assert.Equal(t, "20240101", recalls[0].PublicationDate)
}

func TestFetchRecallsMissingDescription(t *testing.T) {
	t.Parallel()

	client := routedClient(t, map[string]any{
		endpointRecalls:    []VehicleRecall{{Kenteken: "AB12CD", ReferentiecodeRDW: "MGPX"}},
		endpointRecallDesc: []recallDescription{}, // empty: no enrichment
	})

	recalls, err := FetchRecalls(context.Background(), client, "AB12CD")
	require.NoError(t, err)
	require.Len(t, recalls, 1)
	assert.Empty(t, recalls[0].Description)
}

func TestFormatNewSections(t *testing.T) {
	t.Parallel()

	data := &AllVehicleData{
		Base: &VehicleBaseInfo{Kenteken: "AB12CD"},
		Recalls: []VehicleRecall{{
			ReferentiecodeRDW: "MGP1",
			Status:            "Open",
			Description:       "Brake",
			PublicationDate:   "20240102",
		}},
		APKHistory: []VehicleAPKInspection{{
			MeldDatumDoorKeuringsinstantie: "20230315",
			SoortMeldingKIOmschrijving:     "periodieke controle",
			SoortErkenningOmschrijving:     "APK Lichte voertuigen",
			VervaldatumKeuring:             "20250315",
		}},
		Defects: []VehicleDefect{{
			GebrekIdentificatie:            "AC4",
			AantalGebrekenGeconstateerd:    "1",
			MeldDatumDoorKeuringsinstantie: "20230315",
			SoortErkenningOmschrijving:     "APK Lichte voertuigen",
		}},
	}

	out := FormatVehicleInfo(data)
	assert.Contains(t, out, "OPEN RECALLS")
	assert.Contains(t, out, "APK INSPECTION HISTORY")
	assert.Contains(t, out, "REPORTED DEFECTS")
	assert.Contains(t, out, "Brake")
	assert.Contains(t, out, "2023-03-15") // formatted date
	assert.Contains(t, out, "AC4")
}

func TestFormatRDWDate(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":         "Unknown",
		"20240131": "2024-01-31",
		"abcdefgh": "abcdefgh",
		"2024":     "2024",
	}

	for input, want := range cases {
		assert.Equal(t, want, formatRDWDate(input), input)
	}
}
