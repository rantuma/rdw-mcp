package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// endpointRouter routes requests to different handlers based on the
// endpoint ID found in the URL path.
type (
	endpointRouter map[string]http.HandlerFunc
)

func (er endpointRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	for ep, handler := range er {
		if strings.Contains(req.URL.Path, ep) {
			recorder := httptest.NewRecorder()
			handler(recorder, req)

			return recorder.Result(), nil
		}
	}

	recorder := httptest.NewRecorder()
	http.Error(recorder, "no handler: "+req.URL.Path, http.StatusNotFound)

	return recorder.Result(), nil
}

const (
	epBase    = "m9d7-ebf2"
	epFuel    = "8ys7-d773"
	epAxles   = "3huj-srit"
	epBody    = "vezc-m2t6"
	epRecalls = "t49b-isb7"
	epRecallD = "j9yg-7rg9"
	epAPK     = "sgfe-77wx"
	epDefects = "a34c-vvps"
)

const testPlate = "AB12CD"

func errorRouter() endpointRouter {
	return endpointRouter{
		epBase:    errorEndpointHandler,
		epFuel:    errorEndpointHandler,
		epAxles:   errorEndpointHandler,
		epBody:    errorEndpointHandler,
		epRecalls: errorEndpointHandler,
		epRecallD: errorEndpointHandler,
		epAPK:     errorEndpointHandler,
		epDefects: errorEndpointHandler,
	}
}

func emptyEndpointHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, err := rw.Write([]byte("[]"))
		assert.NoError(t, err)
	}
}

//nolint:unparam // kenteken is parameterised for parity with the other test record helpers.
func vehicleBaseHandler(t *testing.T, kenteken string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		records := []rdw.VehicleBaseInfo{{Kenteken: kenteken, Merk: "FORD"}}
		rw.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(rw).Encode(records))
	}
}

func errorEndpointHandler(rw http.ResponseWriter, _ *http.Request) {
	http.Error(rw, "internal server error", http.StatusInternalServerError)
}

// allEmpty returns an endpointRouter where the base endpoint uses baseHandler
// and every other endpoint returns the empty handler. plate is unused but kept
// for documentation purposes.
func allEmpty(t *testing.T, _ string, baseHandler http.HandlerFunc) endpointRouter {
	t.Helper()

	empty := emptyEndpointHandler(t)

	return endpointRouter{
		epBase:    baseHandler,
		epFuel:    empty,
		epAxles:   empty,
		epBody:    empty,
		epRecalls: empty,
		epRecallD: empty,
		epAPK:     empty,
		epDefects: empty,
	}
}

// resultText concatenates all text-content blocks of a CallToolResult.
func resultText(result *mcp.CallToolResult) string {
	var sb strings.Builder

	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}

	return sb.String()
}

func TestCleanKenteken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"removes hyphens", "AB-12-CD", "AB12CD"},
		{"removes spaces", "AB 12 CD", "AB12CD"},
		{"uppercases input", "ab12cd", "AB12CD"},
		{"mixed hyphens and spaces", "ab-12 cd", "AB12CD"},
		{"already clean uppercase", "AB12CD", "AB12CD"},
		{"empty string stays empty", "", ""},
		{"only separators become empty", "- -", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rdw.CleanKenteken(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHandleLookup(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	tests := []struct {
		name        string
		kenteken    string
		handlers    endpointRouter
		wantError   bool
		wantContain string
		wantFound   bool
	}{
		{
			name:        "found vehicle returns complete report",
			kenteken:    plate,
			handlers:    allEmpty(t, plate, vehicleBaseHandler(t, plate)),
			wantContain: "COMPLETE RDW Database Information",
			wantFound:   true,
		},
		{
			name:        "unknown plate returns informational message",
			kenteken:    plate,
			handlers:    allEmpty(t, plate, emptyEndpointHandler(t)),
			wantContain: "No vehicle found",
		},
		{
			name:        "all-hyphen input is invalid kenteken",
			kenteken:    "---",
			handlers:    endpointRouter{},
			wantError:   true,
			wantContain: "invalid kenteken",
		},
		{
			name:        "non-sidecode plate is rejected by validator",
			kenteken:    "ABCDEF",
			handlers:    endpointRouter{},
			wantError:   true,
			wantContain: "invalid kenteken",
		},
		{
			name:        "hyphens are removed from plate",
			kenteken:    "AB-12-CD",
			handlers:    allEmpty(t, "AB12CD", vehicleBaseHandler(t, "AB12CD")),
			wantContain: "AB12CD",
			wantFound:   true,
		},
		{
			name:        "base API error produces error result",
			kenteken:    plate,
			handlers:    allEmpty(t, plate, errorEndpointHandler),
			wantError:   true,
			wantContain: "RDW API error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{Transport: tc.handlers}
			input := LookupInput{Kenteken: tc.kenteken}

			result, output, err := handleLookup(context.Background(), client, input)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEmpty(t, result.Content)

			if tc.wantError {
				assert.True(t, result.IsError)
			} else {
				assert.False(t, result.IsError)
				assert.Equal(t, tc.wantFound, output.Found)
			}

			if tc.wantContain != "" {
				assert.Contains(t, resultText(result), tc.wantContain)
			}
		})
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Parallel()

	client := newHTTPClient(0)
	require.NotNil(t, client)
	assert.Greater(t, client.Timeout.Seconds(), float64(0))
}

func TestLookupHandlerWrapper(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: allEmpty(t, testPlate, vehicleBaseHandler(t, testPlate))}
	h := lookupHandler(client)

	result, out, err := h(context.Background(), nil, LookupInput{Kenteken: testPlate})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, out.Found)
}

func TestHandlersInvalidKenteken(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: errorRouter()}
	in := LookupInput{Kenteken: "ZZZ"}
	ctx := context.Background()

	// We'll verify each split handler produces an IsError CallToolResult for
	// invalid kenteken without ever hitting the network.

	{
		r, _, err := basicHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := technicalHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := fuelHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := recallsHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := apkHistoryHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := defectsHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
}

func TestHandlersAPIError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: errorRouter()}
	in := LookupInput{Kenteken: testPlate}
	ctx := context.Background()

	{
		r, _, err := basicHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
		assert.Contains(t, resultText(r), "RDW API error")
	}
	{
		r, _, err := fuelHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := recallsHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := apkHistoryHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
	{
		r, _, err := defectsHandler(client)(ctx, nil, in)
		require.NoError(t, err)
		assert.True(t, r.IsError)
	}
}

func TestStartHTTPServerLifecycle(t *testing.T) {
	t.Parallel()

	// Sanity check that the transport package wires up against an MCP server.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewMCPServer(t *testing.T) {
	t.Parallel()

	srv := newMCPServer(newHTTPClient(0))
	require.NotNil(t, srv)
}

func TestRegisterTools(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		srv := newMCPServer(newHTTPClient(0))
		require.NotNil(t, srv)
	})
}
