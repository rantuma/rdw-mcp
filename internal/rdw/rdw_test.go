package rdw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// TestMain disables retry/backoff and the response cache so tests run fast and
// remain hermetic.
func TestMain(m *testing.M) {
	rdw.SetClientConfig(rdw.ClientConfig{
		MaxAttempts:    1, // disable retries for fast deterministic tests
		PerCallTimeout: 0, // default (8s)
	})

	os.Exit(m.Run())
}

// redirectTransport rewrites all requests to a fixed test server URL while
// preserving the original path and query string.
// endpointTransport routes requests to different handlers based on the endpoint
// ID in the URL path.
type (
	redirectTransport struct {
		serverURL string
	}

	endpointTransport struct {
		handlers map[string]http.HandlerFunc
	}
)

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"

	// Parse the test server base URL to get its host.
	base := strings.TrimPrefix(rt.serverURL, "http://")
	base = strings.TrimPrefix(base, "https://")
	cloned.URL.Host = base

	return http.DefaultTransport.RoundTrip(cloned)
}

func (et endpointTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for ep, handler := range et.handlers {
		if strings.Contains(req.URL.Path, ep) {
			recorder := httptest.NewRecorder()
			handler(recorder, req)

			return recorder.Result(), nil
		}
	}

	recorder := httptest.NewRecorder()
	http.Error(recorder, "no handler for path: "+req.URL.Path, http.StatusNotFound)

	return recorder.Result(), nil
}

// Endpoint IDs — must match the unexported constants in the rdw package.
const (
	epBase  = "m9d7-ebf2"
	epFuel  = "8ys7-d773"
	epAxles = "3huj-srit"
	epBody  = "vezc-m2t6"
)

func emptyJSONArray(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, err := rw.Write([]byte("[]"))
		assert.NoError(t, err)
	}
}

func internalError(rw http.ResponseWriter, _ *http.Request) {
	http.Error(rw, "internal server error", http.StatusInternalServerError)
}

//nolint:unparam // kenteken is kept as a parameter for parity with fuelRecord/axleRecord/bodyRecord helpers.
func baseRecord(t *testing.T, kenteken string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		record := []rdw.VehicleBaseInfo{{Kenteken: kenteken, Merk: "TOYOTA"}}
		rw.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(rw).Encode(record))
	}
}

func fuelRecord(t *testing.T, kenteken string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		record := []rdw.VehicleFuelInfo{{
			Kenteken:              kenteken,
			BrandstofOmschrijving: "Benzine",
			Nettomaximumvermogen:  "85",
		}}
		rw.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(rw).Encode(record))
	}
}

func axleRecord(t *testing.T, kenteken string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		record := []rdw.VehicleAxesInfo{{Kenteken: kenteken, AsNummer: "1", AantalAssen: "2"}}
		rw.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(rw).Encode(record))
	}
}

func bodyRecord(t *testing.T, kenteken string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		record := []rdw.VehicleBodyInfo{{Kenteken: kenteken, Carrosserietype: "Hatchback"}}
		rw.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(rw).Encode(record))
	}
}

func TestExportMakeRDWRequestBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		respBody   string
		respStatus int
		wantErr    bool
		errContain string
		wantLen    int
	}{
		{
			name:       "success with one record",
			respBody:   `[{"kenteken":"F794KP"}]`,
			respStatus: http.StatusOK,
			wantLen:    1,
		},
		{
			name:       "success with empty array",
			respBody:   `[]`,
			respStatus: http.StatusOK,
			wantLen:    0,
		},
		{
			name:       "non-200 returns error",
			respBody:   `not found`,
			respStatus: http.StatusNotFound,
			wantErr:    true,
			errContain: "404",
		},
		{
			name:       "malformed JSON returns error",
			respBody:   `{invalid`,
			respStatus: http.StatusOK,
			wantErr:    true,
			errContain: "unmarshal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(
				http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					rw.Header().Set("Content-Type", "application/json")
					rw.WriteHeader(tc.respStatus)
					_, err := rw.Write([]byte(tc.respBody))
					assert.NoError(t, err)
				}),
			)
			defer srv.Close()

			client := &http.Client{Transport: redirectTransport{serverURL: srv.URL}}
			ctx := context.Background()

			results, err := rdw.ExportMakeRDWRequestBase(ctx, client, epBase, "AB1234")

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContain)

				return
			}

			require.NoError(t, err)
			assert.Len(t, results, tc.wantLen)
		})
	}
}

func TestExportMakeRDWRequestBase_UserAgent(t *testing.T) {
	t.Parallel()

	var gotUserAgent string

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotUserAgent = req.Header.Get("User-Agent")
		rw.Header().Set("Content-Type", "application/json")
		_, err := rw.Write([]byte("[]"))
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := &http.Client{Transport: redirectTransport{serverURL: srv.URL}}

	_, err := rdw.ExportMakeRDWRequestBase(context.Background(), client, epBase, "F794KP")
	require.NoError(t, err)
	assert.Equal(t, rdw.UserAgent, gotUserAgent)
}

func TestExportMakeRDWRequestBase_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &http.Client{}
	_, err := rdw.ExportMakeRDWRequestBase(ctx, client, epBase, "F794KP")

	require.Error(t, err)
}

func TestFetchAllVehicleData(t *testing.T) {
	t.Parallel()

	const plate = "F794KP"

	tests := []struct {
		name        string
		handlers    map[string]http.HandlerFunc
		wantErr     bool
		wantNilBase bool
		wantFuel    int
		wantAxles   int
		wantBody    int
		wantPower   string
	}{
		{
			name: "vehicle not found — empty base returns nil",
			handlers: map[string]http.HandlerFunc{
				epBase:  emptyJSONArray(t),
				epFuel:  emptyJSONArray(t),
				epAxles: emptyJSONArray(t),
				epBody:  emptyJSONArray(t),
			},
			wantNilBase: true,
		},
		{
			name: "base endpoint error propagates as error",
			handlers: map[string]http.HandlerFunc{
				epBase:  internalError,
				epFuel:  emptyJSONArray(t),
				epAxles: emptyJSONArray(t),
				epBody:  emptyJSONArray(t),
			},
			wantErr: true,
		},
		{
			name: "all endpoints succeed",
			handlers: map[string]http.HandlerFunc{
				epBase:  baseRecord(t, plate),
				epFuel:  fuelRecord(t, plate),
				epAxles: axleRecord(t, plate),
				epBody:  bodyRecord(t, plate),
			},
			wantFuel:  1,
			wantAxles: 1,
			wantBody:  1,
			wantPower: "85",
		},
		{
			name: "secondary endpoint errors are non-fatal",
			handlers: map[string]http.HandlerFunc{
				epBase:  baseRecord(t, plate),
				epFuel:  internalError,
				epAxles: internalError,
				epBody:  internalError,
			},
			wantFuel:  0,
			wantAxles: 0,
			wantBody:  0,
		},
		{
			name: "fuel power merges into base when base has none",
			handlers: map[string]http.HandlerFunc{
				epBase:  baseRecord(t, plate),
				epFuel:  fuelRecord(t, plate),
				epAxles: emptyJSONArray(t),
				epBody:  emptyJSONArray(t),
			},
			wantFuel:  1,
			wantPower: "85",
		},
		{
			name: "empty secondary endpoints are non-fatal",
			handlers: map[string]http.HandlerFunc{
				epBase:  baseRecord(t, plate),
				epFuel:  emptyJSONArray(t),
				epAxles: emptyJSONArray(t),
				epBody:  emptyJSONArray(t),
			},
			wantFuel:  0,
			wantAxles: 0,
			wantBody:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{Transport: endpointTransport{handlers: tc.handlers}}
			data, err := rdw.FetchAllVehicleData(context.Background(), client, plate)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			if tc.wantNilBase {
				assert.Nil(t, data.Base)

				return
			}

			require.NotNil(t, data.Base)
			assert.Len(t, data.Fuel, tc.wantFuel)
			assert.Len(t, data.Axles, tc.wantAxles)
			assert.Len(t, data.Body, tc.wantBody)

			if tc.wantPower != "" {
				assert.Equal(t, tc.wantPower, data.Base.Nettomaximumvermogen)
			}
		})
	}
}

func TestExportBuildResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		base         rdw.VehicleBaseInfo
		fuelRecords  []rdw.VehicleFuelInfo
		fuelErr      error
		axleRecords  []rdw.VehicleAxesInfo
		axleErr      error
		bodyRecords  []rdw.VehicleBodyInfo
		bodyErr      error
		wantFuelLen  int
		wantAxlesLen int
		wantBodyLen  int
		wantPower    string
	}{
		{
			name: "all data present, no errors",
			base: rdw.VehicleBaseInfo{Kenteken: "F794KP"},
			fuelRecords: []rdw.VehicleFuelInfo{
				{BrandstofOmschrijving: "Benzine", Nettomaximumvermogen: "75"},
			},
			axleRecords:  []rdw.VehicleAxesInfo{{AsNummer: "1"}},
			bodyRecords:  []rdw.VehicleBodyInfo{{Carrosserietype: "Hatchback"}},
			wantFuelLen:  1,
			wantAxlesLen: 1,
			wantBodyLen:  1,
			wantPower:    "75",
		},
		{
			name:         "errors cause empty slices",
			base:         rdw.VehicleBaseInfo{Kenteken: "F794KP"},
			fuelErr:      assert.AnError,
			axleErr:      assert.AnError,
			bodyErr:      assert.AnError,
			wantFuelLen:  0,
			wantAxlesLen: 0,
			wantBodyLen:  0,
		},
		{
			name:        "fuel power merges into base",
			base:        rdw.VehicleBaseInfo{Kenteken: "9KHX31"},
			fuelRecords: []rdw.VehicleFuelInfo{{Nettomaximumvermogen: "100"}},
			wantFuelLen: 1,
			wantPower:   "100",
		},
		{
			name:        "empty fuel slice, no merge",
			base:        rdw.VehicleBaseInfo{Kenteken: "9KHX31", Nettomaximumvermogen: "50"},
			fuelRecords: []rdw.VehicleFuelInfo{},
			wantFuelLen: 0,
			wantPower:   "50",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := rdw.ExportBuildResult(
				tc.base,
				tc.fuelRecords, tc.fuelErr,
				tc.axleRecords, tc.axleErr,
				tc.bodyRecords, tc.bodyErr,
			)

			require.NotNil(t, result)
			require.NotNil(t, result.Base)
			assert.Len(t, result.Fuel, tc.wantFuelLen)
			assert.Len(t, result.Axles, tc.wantAxlesLen)
			assert.Len(t, result.Body, tc.wantBodyLen)

			if tc.wantPower != "" {
				assert.Equal(t, tc.wantPower, result.Base.Nettomaximumvermogen)
			}
		})
	}
}
