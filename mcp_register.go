package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// Tool / resource / prompt identifiers exposed to MCP clients.
const (
	toolNameFull          = "rdw_vehicle_full"
	toolNameLegacy        = "rdw-license-plate-lookup"
	toolNameBasic         = "rdw_vehicle_basic"
	toolNameTechnical     = "rdw_vehicle_technical"
	toolNameFuelEmissions = "rdw_vehicle_fuel_emissions"
	toolNameRecalls       = "rdw_vehicle_recalls"
	toolNameAPKHistory    = "rdw_vehicle_apk_history"
	toolNameDefects       = "rdw_vehicle_defects"

	resourceTemplateURI = "rdw://kenteken/{plate}"
	resourceURIPrefix   = "rdw://kenteken/"
	resourceMIMEType    = "application/json"

	promptNameSummarize = "summarize_vehicle"
)

// registeredToolNames is the closed set of tool names this server exposes. It is
// the source of truth for telemetry: only these names may appear in the
// mcp.tool metric label, so a client cannot mint unbounded time series by
// calling tools/call with arbitrary names. Keep in sync with registerAll.
//
//nolint:gochecknoglobals // immutable lookup table for the fixed tool set
var registeredToolNames = map[string]struct{}{
	toolNameFull:          {},
	toolNameLegacy:        {},
	toolNameBasic:         {},
	toolNameTechnical:     {},
	toolNameFuelEmissions: {},
	toolNameRecalls:       {},
	toolNameAPKHistory:    {},
	toolNameDefects:       {},
}

// isRegisteredTool reports whether name is one of the tools this server exposes.
func isRegisteredTool(name string) bool {
	_, ok := registeredToolNames[name]

	return ok
}

// Tool descriptions.
const (
	descFull = "Look up ALL available Dutch vehicle information from RDW datasets " +
		"(base registration, fuel & emissions, axles, body, recalls, APK history, defects) " +
		"for the given kenteken."
	descBasic = "Look up basic Dutch vehicle registration data (brand, model, colour, " +
		"first registration date, registration status) for the given kenteken."
	descTechnical = "Look up technical specifications (engine, dimensions, weights, axles, " +
		"body) for the given Dutch kenteken."
	descFuelEmissions = "Look up fuel and emissions records (fuel type, CO2, consumption, " +
		"emission class) for the given Dutch kenteken."
	descRecalls    = "Look up open RDW recall actions for the given Dutch kenteken."
	descAPKHistory = "Look up APK (MOT) inspection history for the given Dutch kenteken."
	descDefects    = "Look up reported defects from past inspections for the given Dutch kenteken."
)

type (
	// LookupInput is the shared input schema for all per-kenteken tools.
	LookupInput struct {
		Kenteken string `json:"kenteken" jsonschema:"Dutch license plate (kenteken) to look up"`
	}

	// LookupOutput is the structured output payload for the full lookup tool.
	LookupOutput struct {
		Kenteken   string                     `json:"kenteken"              jsonschema:"normalized kenteken that was queried"`
		Found      bool                       `json:"found"                 jsonschema:"whether a vehicle was found for the kenteken"`
		Base       *rdw.VehicleBaseInfo       `json:"base,omitempty"        jsonschema:"base registration record"`
		Fuel       []rdw.VehicleFuelInfo      `json:"fuel,omitempty"        jsonschema:"fuel and emissions records"`
		Axles      []rdw.VehicleAxesInfo      `json:"axles,omitempty"       jsonschema:"axle specification records"`
		Body       []rdw.VehicleBodyInfo      `json:"body,omitempty"        jsonschema:"body (carrosserie) records"`
		Recalls    []rdw.VehicleRecall        `json:"recalls,omitempty"     jsonschema:"open RDW recall actions"`
		APKHistory []rdw.VehicleAPKInspection `json:"apk_history,omitempty" jsonschema:"APK (MOT) inspection history"`
		Defects    []rdw.VehicleDefect        `json:"defects,omitempty"     jsonschema:"reported defects"`
		Report     string                     `json:"report"                jsonschema:"human-readable multi-section report"`
	}

	// BasicOutput is the output of rdw_vehicle_basic.
	BasicOutput struct {
		Kenteken string               `json:"kenteken"`
		Found    bool                 `json:"found"`
		Base     *rdw.VehicleBaseInfo `json:"base,omitempty"`
		Report   string               `json:"report"`
	}

	// TechnicalOutput is the output of rdw_vehicle_technical.
	TechnicalOutput struct {
		Kenteken string                `json:"kenteken"`
		Found    bool                  `json:"found"`
		Base     *rdw.VehicleBaseInfo  `json:"base,omitempty"`
		Axles    []rdw.VehicleAxesInfo `json:"axles,omitempty"`
		Body     []rdw.VehicleBodyInfo `json:"body,omitempty"`
		Report   string                `json:"report"`
	}

	// FuelOutput is the output of rdw_vehicle_fuel_emissions.
	FuelOutput struct {
		Kenteken string                `json:"kenteken"`
		Fuel     []rdw.VehicleFuelInfo `json:"fuel,omitempty"`
		Report   string                `json:"report"`
	}

	// RecallsOutput is the output of rdw_vehicle_recalls.
	RecallsOutput struct {
		Kenteken string              `json:"kenteken"`
		Recalls  []rdw.VehicleRecall `json:"recalls,omitempty"`
		Report   string              `json:"report"`
	}

	// APKHistoryOutput is the output of rdw_vehicle_apk_history.
	APKHistoryOutput struct {
		Kenteken string                     `json:"kenteken"`
		History  []rdw.VehicleAPKInspection `json:"history,omitempty"`
		Report   string                     `json:"report"`
	}

	// DefectsOutput is the output of rdw_vehicle_defects.
	DefectsOutput struct {
		Kenteken string              `json:"kenteken"`
		Defects  []rdw.VehicleDefect `json:"defects,omitempty"`
		Report   string              `json:"report"`
	}
)

// registerAll registers every tool, resource, and prompt on srv.
func registerAll(srv *mcp.Server, client *http.Client) {
	registerTools(srv, client)
	registerResources(srv, client)
	registerPrompts(srv)
}

// registerTools binds every RDW lookup tool on srv.
func registerTools(srv *mcp.Server, client *http.Client) {
	full := lookupHandler(client)
	mcp.AddTool(srv, &mcp.Tool{Name: toolNameFull, Description: descFull}, full)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolNameLegacy,
		Description: descFull + " (deprecated alias for " + toolNameFull + ")",
	}, full)

	mcp.AddTool(srv, &mcp.Tool{Name: toolNameBasic, Description: descBasic}, basicHandler(client))
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: toolNameTechnical, Description: descTechnical},
		technicalHandler(client),
	)
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: toolNameFuelEmissions, Description: descFuelEmissions},
		fuelHandler(client),
	)
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: toolNameRecalls, Description: descRecalls},
		recallsHandler(client),
	)
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: toolNameAPKHistory, Description: descAPKHistory},
		apkHistoryHandler(client),
	)
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: toolNameDefects, Description: descDefects},
		defectsHandler(client),
	)
}

// registerResources publishes the rdw://kenteken/{plate} resource template.
func registerResources(srv *mcp.Server, client *http.Client) {
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceTemplateURI,
		Name:        "rdw_vehicle",
		Description: "Full RDW vehicle dataset (JSON) for the given kenteken.",
		MIMEType:    resourceMIMEType,
	}, vehicleResourceHandler(client))
}

// registerPrompts publishes the summarize_vehicle prompt.
func registerPrompts(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        promptNameSummarize,
		Title:       "Summarize a Dutch vehicle",
		Description: "Generate an instruction prompt that asks the model to summarise an RDW vehicle by kenteken.",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "kenteken",
				Description: "Dutch license plate to summarise.",
				Required:    true,
			},
		},
	}, summarizeVehiclePromptHandler)
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

func lookupHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, LookupOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in LookupInput) (*mcp.CallToolResult, LookupOutput, error) {
		return handleLookup(ctx, client, in)
	}
}

func handleLookup(
	ctx context.Context,
	client *http.Client,
	input LookupInput,
) (*mcp.CallToolResult, LookupOutput, error) {
	kenteken, errResult := normalizeKenteken(input.Kenteken)
	if errResult != nil {
		return errResult, LookupOutput{}, nil
	}

	data, err := rdw.FetchAllVehicleData(ctx, client, kenteken)
	if err != nil {
		//nolint:nilerr // intentional: surface as IsError tool result
		return errorResult("RDW API error: " + err.Error()), LookupOutput{}, nil
	}

	if data.Base == nil {
		msg := notFoundMsg(kenteken)
		return textResult(msg), LookupOutput{Kenteken: kenteken, Found: false, Report: msg}, nil
	}

	report := fmt.Sprintf(
		"COMPLETE RDW Database Information for %s:\n\n%s",
		kenteken,
		rdw.FormatVehicleInfo(data),
	)

	out := LookupOutput{
		Kenteken:   kenteken,
		Found:      true,
		Base:       data.Base,
		Fuel:       data.Fuel,
		Axles:      data.Axles,
		Body:       data.Body,
		Recalls:    data.Recalls,
		APKHistory: data.APKHistory,
		Defects:    data.Defects,
		Report:     report,
	}

	return textResult(report), out, nil
}

func basicHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, BasicOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in LookupInput) (*mcp.CallToolResult, BasicOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, BasicOutput{}, nil
		}

		base, err := rdw.FetchBase(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), BasicOutput{}, nil
		}

		if base == nil {
			msg := notFoundMsg(kenteken)
			return textResult(msg), BasicOutput{Kenteken: kenteken, Found: false, Report: msg}, nil
		}

		report := rdw.FormatVehicleInfo(&rdw.AllVehicleData{Base: base})
		out := BasicOutput{Kenteken: kenteken, Found: true, Base: base, Report: report}

		return textResult(report), out, nil
	}
}

func technicalHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, TechnicalOutput, error) {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in LookupInput,
	) (*mcp.CallToolResult, TechnicalOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, TechnicalOutput{}, nil
		}

		base, err := rdw.FetchBase(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), TechnicalOutput{}, nil
		}

		if base == nil {
			msg := notFoundMsg(kenteken)
			return textResult(
					msg,
				), TechnicalOutput{
					Kenteken: kenteken,
					Found:    false,
					Report:   msg,
				}, nil
		}

		axles, _ := rdw.FetchAxles(ctx, client, kenteken)
		body, _ := rdw.FetchBody(ctx, client, kenteken)

		data := &rdw.AllVehicleData{Base: base, Axles: axles, Body: body}
		report := rdw.FormatVehicleInfo(data)
		out := TechnicalOutput{
			Kenteken: kenteken,
			Found:    true,
			Base:     base,
			Axles:    axles,
			Body:     body,
			Report:   report,
		}

		return textResult(report), out, nil
	}
}

//nolint:dupl // single-section handlers share structure intentionally
func fuelHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, FuelOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in LookupInput) (*mcp.CallToolResult, FuelOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, FuelOutput{}, nil
		}

		fuel, err := rdw.FetchFuel(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), FuelOutput{}, nil
		}

		report := renderListReport(kenteken, "fuel/emissions records", len(fuel), func() string {
			return rdw.FormatVehicleInfo(
				&rdw.AllVehicleData{Base: &rdw.VehicleBaseInfo{Kenteken: kenteken}, Fuel: fuel},
			)
		})
		out := FuelOutput{Kenteken: kenteken, Fuel: fuel, Report: report}

		return textResult(report), out, nil
	}
}

//nolint:dupl // single-section handlers share structure intentionally
func recallsHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, RecallsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in LookupInput) (*mcp.CallToolResult, RecallsOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, RecallsOutput{}, nil
		}

		recalls, err := rdw.FetchRecalls(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), RecallsOutput{}, nil
		}

		report := renderListReport(kenteken, "open recall actions", len(recalls), func() string {
			return rdw.FormatVehicleInfo(
				&rdw.AllVehicleData{
					Base:    &rdw.VehicleBaseInfo{Kenteken: kenteken},
					Recalls: recalls,
				},
			)
		})
		out := RecallsOutput{Kenteken: kenteken, Recalls: recalls, Report: report}

		return textResult(report), out, nil
	}
}

//nolint:dupl // single-section handlers share structure intentionally
func apkHistoryHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, APKHistoryOutput, error) {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in LookupInput,
	) (*mcp.CallToolResult, APKHistoryOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, APKHistoryOutput{}, nil
		}

		history, err := rdw.FetchAPKHistory(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), APKHistoryOutput{}, nil
		}

		report := renderListReport(kenteken, "APK inspection records", len(history), func() string {
			return rdw.FormatVehicleInfo(&rdw.AllVehicleData{
				Base: &rdw.VehicleBaseInfo{Kenteken: kenteken}, APKHistory: history,
			})
		})
		out := APKHistoryOutput{Kenteken: kenteken, History: history, Report: report}

		return textResult(report), out, nil
	}
}

//nolint:dupl // single-section handlers share structure intentionally
func defectsHandler(
	client *http.Client,
) func(context.Context, *mcp.CallToolRequest, LookupInput) (*mcp.CallToolResult, DefectsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in LookupInput) (*mcp.CallToolResult, DefectsOutput, error) {
		kenteken, errResult := normalizeKenteken(in.Kenteken)
		if errResult != nil {
			return errResult, DefectsOutput{}, nil
		}

		defects, err := rdw.FetchDefects(ctx, client, kenteken)
		if err != nil {
			//nolint:nilerr // intentional: surface as IsError tool result
			return errorResult("RDW API error: " + err.Error()), DefectsOutput{}, nil
		}

		report := renderListReport(kenteken, "reported defects", len(defects), func() string {
			return rdw.FormatVehicleInfo(&rdw.AllVehicleData{
				Base: &rdw.VehicleBaseInfo{Kenteken: kenteken}, Defects: defects,
			})
		})
		out := DefectsOutput{Kenteken: kenteken, Defects: defects, Report: report}

		return textResult(report), out, nil
	}
}

// ---------------------------------------------------------------------------
// Resource handler
// ---------------------------------------------------------------------------

func vehicleResourceHandler(
	client *http.Client,
) func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI

		raw := strings.TrimPrefix(uri, resourceURIPrefix)
		if raw == uri || raw == "" {
			return nil, fmt.Errorf("resource URI %q does not match %q", uri, resourceTemplateURI)
		}

		kenteken, err := rdw.ValidateKenteken(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid kenteken in resource URI: %w", err)
		}

		data, err := rdw.FetchAllVehicleData(ctx, client, kenteken)
		if err != nil {
			return nil, fmt.Errorf("rdw fetch: %w", err)
		}

		out := LookupOutput{Kenteken: kenteken}
		if data.Base != nil {
			out.Found = true
			out.Base = data.Base
			out.Fuel = data.Fuel
			out.Axles = data.Axles
			out.Body = data.Body
			out.Recalls = data.Recalls
			out.APKHistory = data.APKHistory
			out.Defects = data.Defects
			out.Report = rdw.FormatVehicleInfo(data)
		} else {
			out.Report = notFoundMsg(kenteken)
		}

		body, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal resource: %w", err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: uri, MIMEType: resourceMIMEType, Text: string(body)},
			},
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Prompt handler
// ---------------------------------------------------------------------------

func summarizeVehiclePromptHandler(
	_ context.Context,
	req *mcp.GetPromptRequest,
) (*mcp.GetPromptResult, error) {
	kenteken := strings.TrimSpace(req.Params.Arguments["kenteken"])
	if kenteken == "" {
		return nil, errors.New("missing required argument: kenteken")
	}

	cleaned, err := rdw.ValidateKenteken(kenteken)
	if err != nil {
		return nil, fmt.Errorf("invalid kenteken argument: %w", err)
	}

	text := fmt.Sprintf(
		"Summarise the Dutch vehicle with kenteken %s.\n\n"+
			"1. Call the `%s` tool (or read the `%s%s` resource) to fetch the full RDW dataset.\n"+
			"2. Produce a concise summary covering:\n"+
			"   - brand, trade name, model year and body type\n"+
			"   - first registration in the Netherlands and current registration status\n"+
			"   - key technical specs (engine, weight, towing capacity)\n"+
			"   - fuel type, CO2 emissions and emission class\n"+
			"   - any open recalls or recently reported defects\n"+
			"   - APK (MOT) status and next inspection date\n"+
			"3. Highlight anything safety- or compliance-relevant.\n",
		cleaned,
		toolNameFull,
		resourceURIPrefix,
		cleaned,
	)

	return &mcp.GetPromptResult{
		Description: "Summarise an RDW vehicle by kenteken.",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: text}},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// normalizeKenteken cleans + validates the input. On failure it returns a
// populated CallToolResult so the caller can return early.
func normalizeKenteken(raw string) (string, *mcp.CallToolResult) {
	cleaned, err := rdw.ValidateKenteken(raw)
	if err != nil {
		return "", errorResult(err.Error())
	}

	return cleaned, nil
}

// notFoundMsg formats a "no vehicle found" message.
func notFoundMsg(kenteken string) string {
	return fmt.Sprintf("No vehicle found for license plate: %s", kenteken)
}

// renderListReport wraps a section formatter with a per-tool header. When
// count==0 it returns a stand-alone "no records" message.
func renderListReport(kenteken, what string, count int, format func() string) string {
	if count == 0 {
		return fmt.Sprintf("No %s found for license plate: %s", what, kenteken)
	}

	return fmt.Sprintf("RDW %s for %s:\n\n%s", what, kenteken, format())
}

// textResult builds a successful CallToolResult containing a single text block.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// errorResult builds an error CallToolResult containing a single text block.
func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
