package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicHandler(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: allEmpty(t, plate, vehicleBaseHandler(t, plate))}
		handler := basicHandler(client)

		result, out, err := handler(context.Background(), nil, LookupInput{Kenteken: plate})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.True(t, out.Found)
		assert.Equal(t, plate, out.Kenteken)
		require.NotNil(t, out.Base)
		assert.Equal(t, "FORD", out.Base.Merk)
	})

	t.Run("invalid kenteken", func(t *testing.T) {
		t.Parallel()
		handler := basicHandler(&http.Client{})

		result, _, err := handler(context.Background(), nil, LookupInput{Kenteken: "ZZZ"})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, resultText(result), "invalid kenteken")
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: allEmpty(t, plate, emptyEndpointHandler(t))}
		handler := basicHandler(client)

		result, out, err := handler(context.Background(), nil, LookupInput{Kenteken: plate})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.False(t, out.Found)
		assert.Contains(t, resultText(result), "No vehicle found")
	})
}

func TestTechnicalHandler(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	client := &http.Client{Transport: allEmpty(t, plate, vehicleBaseHandler(t, plate))}
	handler := technicalHandler(client)

	result, out, err := handler(context.Background(), nil, LookupInput{Kenteken: plate})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.True(t, out.Found)
	assert.NotNil(t, out.Base)
}

func TestFuelHandler(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	t.Run("empty fuel", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: allEmpty(t, plate, emptyEndpointHandler(t))}
		handler := fuelHandler(client)

		result, out, err := handler(context.Background(), nil, LookupInput{Kenteken: plate})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, out.Fuel)
		assert.Contains(t, resultText(result), "No fuel/emissions records")
	})
}

func TestRecallsAPKDefectsHandlers(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	router := allEmpty(t, plate, emptyEndpointHandler(t))
	client := &http.Client{Transport: router}

	for _, h := range []func(*http.Client) any{
		func(c *http.Client) any { return recallsHandler(c) },
		func(c *http.Client) any { return apkHistoryHandler(c) },
		func(c *http.Client) any { return defectsHandler(c) },
	} {
		got := h(client) // just verify no panic on construction
		require.NotNil(t, got)
	}

	// invoke each handler and assert empty-set message
	{
		result, out, err := recallsHandler(
			client,
		)(
			context.Background(),
			nil,
			LookupInput{Kenteken: plate},
		)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, out.Recalls)
		assert.Contains(t, resultText(result), "No open recall actions")
	}
	{
		result, out, err := apkHistoryHandler(
			client,
		)(
			context.Background(),
			nil,
			LookupInput{Kenteken: plate},
		)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, out.History)
		assert.Contains(t, resultText(result), "No APK inspection records")
	}
	{
		result, out, err := defectsHandler(
			client,
		)(
			context.Background(),
			nil,
			LookupInput{Kenteken: plate},
		)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, out.Defects)
		assert.Contains(t, resultText(result), "No reported defects")
	}
}

func TestVehicleResourceHandler(t *testing.T) {
	t.Parallel()

	const plate = "AB12CD"

	t.Run("found marshals JSON", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: allEmpty(t, plate, vehicleBaseHandler(t, plate))}
		handler := vehicleResourceHandler(client)

		req := &mcp.ReadResourceRequest{
			Params: &mcp.ReadResourceParams{URI: "rdw://kenteken/" + plate},
		}
		result, err := handler(context.Background(), req)
		require.NoError(t, err)
		require.Len(t, result.Contents, 1)
		assert.Equal(t, "application/json", result.Contents[0].MIMEType)

		var out LookupOutput
		require.NoError(t, json.Unmarshal([]byte(result.Contents[0].Text), &out))
		assert.True(t, out.Found)
		assert.Equal(t, plate, out.Kenteken)
	})

	t.Run("invalid URI", func(t *testing.T) {
		t.Parallel()
		handler := vehicleResourceHandler(&http.Client{})
		req := &mcp.ReadResourceRequest{
			Params: &mcp.ReadResourceParams{URI: "https://example.com/" + plate},
		}
		_, err := handler(context.Background(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("invalid kenteken in URI", func(t *testing.T) {
		t.Parallel()
		handler := vehicleResourceHandler(&http.Client{})
		req := &mcp.ReadResourceRequest{
			Params: &mcp.ReadResourceParams{URI: "rdw://kenteken/INVALID"},
		}
		_, err := handler(context.Background(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid kenteken")
	})
}

func TestSummarizeVehiclePromptHandler(t *testing.T) {
	t.Parallel()

	t.Run("valid kenteken", func(t *testing.T) {
		t.Parallel()
		req := &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{
				Name:      promptNameSummarize,
				Arguments: map[string]string{"kenteken": "AB-12-CD"},
			},
		}
		result, err := summarizeVehiclePromptHandler(context.Background(), req)
		require.NoError(t, err)
		require.NotEmpty(t, result.Messages)
		text := result.Messages[0].Content.(*mcp.TextContent).Text
		assert.Contains(t, text, "AB12CD")
		assert.Contains(t, text, toolNameFull)
	})

	t.Run("missing argument", func(t *testing.T) {
		t.Parallel()
		req := &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{Name: promptNameSummarize, Arguments: map[string]string{}},
		}
		_, err := summarizeVehiclePromptHandler(context.Background(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required")
	})

	t.Run("invalid kenteken argument", func(t *testing.T) {
		t.Parallel()
		req := &mcp.GetPromptRequest{
			Params: &mcp.GetPromptParams{
				Name:      promptNameSummarize,
				Arguments: map[string]string{"kenteken": "BOGUS"},
			},
		}
		_, err := summarizeVehiclePromptHandler(context.Background(), req)
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "invalid kenteken")
	})
}
