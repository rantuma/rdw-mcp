// Package rdw provides types, an HTTP client, and formatters for the Dutch
// RDW open vehicle registration API.
package rdw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RDW API endpoint identifiers and HTTP client configuration.
const (
	APIBase            = "https://opendata.rdw.nl/resource"
	endpointBase       = "m9d7-ebf2"
	endpointFuel       = "8ys7-d773"
	endpointAxles      = "3huj-srit"
	endpointBody       = "vezc-m2t6"
	endpointRecalls    = "t49b-isb7" // Open recall actions per kenteken.
	endpointRecallDesc = "j9yg-7rg9" // Recall descriptions keyed by referentiecode_rdw.
	endpointAPK        = "sgfe-77wx" // APK inspection history per kenteken.
	endpointDefects    = "a34c-vvps" // Observed defects per kenteken.
	// HTTPTimeout is the default per-request timeout for the RDW HTTP client.
	HTTPTimeout = 10 * time.Second
	// DefaultCacheTTL is the default TTL for cached RDW responses.
	DefaultCacheTTL = 6 * time.Hour
	// DefaultCacheSize is the default LRU capacity (entries).
	DefaultCacheSize = 1024
	// DefaultMaxAttempts is the default total number of attempts (initial +
	// retries) used when ClientConfig.MaxAttempts is unset.
	DefaultMaxAttempts = 3
	// DefaultBaseBackoff is the initial exponential-backoff delay.
	DefaultBaseBackoff = 200 * time.Millisecond
	// DefaultMaxBackoff caps exponential backoff per retry.
	DefaultMaxBackoff = 2 * time.Second
	// DefaultPerCallTimeout bounds an individual endpoint call (incl. retries).
	DefaultPerCallTimeout = 8 * time.Second
	drivenAxleYes         = "J"
	drivenAxleNo          = "N"
)

// UserAgent is the HTTP User-Agent header sent with every RDW API request.
// It is overridable via SetUserAgent so the binary can inject the build version.
var UserAgent = "RDW-MCP-Server/dev" //nolint:gochecknoglobals // intentional injectable build identity

// SetUserAgent overrides the default User-Agent header used for RDW API
// requests. Intended to be called once at startup with a build-time version.
func SetUserAgent(ua string) {
	if ua == "" {
		return
	}

	UserAgent = ua
}

type (
	// VehicleBaseInfo holds the primary RDW registration record for a vehicle.
	VehicleBaseInfo struct {
		Kenteken                             string `json:"kenteken"`
		Voertuigsoort                        string `json:"voertuigsoort"`
		Merk                                 string `json:"merk"`
		Handelsbenaming                      string `json:"handelsbenaming"`
		TypeGoedkeuringNummer                string `json:"type_goedkeuring_nummer"`
		Variant                              string `json:"variant"`
		Uitvoering                           string `json:"uitvoering"`
		EersteKleur                          string `json:"eerste_kleur"`
		TweedeKleur                          string `json:"tweede_kleur"`
		AantalZitplaatsen                    string `json:"aantal_zitplaatsen"`
		AantalStaanplaatsen                  string `json:"aantal_staanplaatsen"`
		DatumEersteToelating                 string `json:"datum_eerste_toelating"`
		DatumEersteTenaamstellingInNederland string `json:"datum_eerste_tenaamstelling_in_nederland"`
		VervaldatumApk                       string `json:"vervaldatum_apk"`
		Inrichting                           string `json:"inrichting"`
		AantalDeuren                         string `json:"aantal_deuren"`
		AantalWielen                         string `json:"aantal_wielen"`
		MassaLedigVoertuig                   string `json:"massa_ledig_voertuig"`
		MassaRijklaar                        string `json:"massa_rijklaar"`
		MaximumMassaTrekkenOngeremd          string `json:"maximum_massa_trekken_ongeremd"`
		MaximumMassaTrekkenGeremd            string `json:"maximum_massa_trekken_geremd"`
		MaximumTrekkenMassaGeremd            string `json:"maximum_trekken_massa_geremd"`
		DatumTenaamstelling                  string `json:"datum_tenaamstelling"`
		BrutoBpm                             string `json:"bruto_bpm"`
		Zuinigheidsclassificatie             string `json:"zuinigheidsclassificatie"`
		Zuinigheidslabel                     string `json:"zuinigheidslabel"`
		ExportIndicator                      string `json:"export_indicator"`
		Exportindicator                      string `json:"exportindicator"`
		OpenstaandeTerugroepactieIndicator   string `json:"openstaande_terugroepactie_indicator"`
		VervaldatumTachograaf                string `json:"vervaldatum_tachograaf"`
		TaxiIndicator                        string `json:"taxi_indicator"`
		MaximumMassaVoertuig                 string `json:"maximum_massa_voertuig"`
		Catalogusprijs                       string `json:"catalogusprijs"`
		WamVerzekerd                         string `json:"wam_verzekerd"`
		AantalCilinders                      string `json:"aantal_cilinders"`
		Cilinderinhoud                       string `json:"cilinderinhoud"`
		MassaAltAandr                        string `json:"massa_alt_aandr"`
		Nettomaximumvermogen                 string `json:"nettomaximumvermogen"`
		NominaalContinuMaximumvermogen       string `json:"nominaal_continu_maximumvermogen"`
		ToegestaneMaximumMassaVoertuig       string `json:"toegestane_maximum_massa_voertuig"`
		TechnischeMaxMassaVoertuig           string `json:"technische_max_massa_voertuig"`
		MaximumMassaSamenstelling            string `json:"maximum_massa_samenstelling"`
		VermogenMassarijklaar                string `json:"vermogen_massarijklaar"`
		Typegoedkeuringsnummer               string `json:"typegoedkeuringsnummer"`
		JaarLaatsteRegistratieTellerstand    string `json:"jaar_laatste_registratie_tellerstand"`
		Type                                 string `json:"type"`
		MaximaleConstructiesnelheid          string `json:"maximale_constructiesnelheid"`
		Lengte                               string `json:"lengte"`
		Breedte                              string `json:"breedte"`
		HoogteVoertuig                       string `json:"hoogte_voertuig"`
		EuropeseVoertuigcategorie            string `json:"europese_voertuigcategorie"`
		VolgnummerWijzigingEuTypegoedkeuring string `json:"volgnummer_wijziging_eu_typegoedkeuring"`
		Wielbasis                            string `json:"wielbasis"`
		Tellerstandoordeel                   string `json:"tellerstandoordeel"`
		CodeToelichtingTellerstandoordeel    string `json:"code_toelichting_tellerstandoordeel"`
		TenaamstellenMogelijk                string `json:"tenaamstellen_mogelijk"`
		WachtOpKeuren                        string `json:"wacht_op_keuren"`
		RegistratieDatumGoedkeuringBpm       string `json:"registratie_datum_goedkeuring_afschrijvingsmoment_bpm"`
		AerodynamicVoorziening               string `json:"aerodyn_voorz"`
		VerlengdeCabineIndicator             string `json:"verl_cab_ind"`
	}

	// VehicleFuelInfo holds fuel type, emissions, and power data for one fuel entry.
	VehicleFuelInfo struct {
		Kenteken                       string `json:"kenteken"`
		BrandstofVolgnummer            string `json:"brandstof_volgnummer"`
		BrandstofOmschrijving          string `json:"brandstof_omschrijving"`
		EmissiecodeOmschrijving        string `json:"emissiecode_omschrijving"`
		Uitlaatemissieniveau           string `json:"uitlaatemissieniveau"`
		Co2Emissieklasse               string `json:"co2_emissieklasse"`
		Nettomaximumvermogen           string `json:"nettomaximumvermogen"`
		GeluidsniveauRijdend           string `json:"geluidsniveau_rijdend"`
		GeluidsniveauStationair        string `json:"geluidsniveau_stationair"`
		Roetuitstoot                   string `json:"roetuitstoot"`
		MilieuklasseEgGoedkeuringLicht string `json:"milieuklasse_eg_goedkeuring_licht"`
		ToerentalGeluidsniveau         string `json:"toerental_geluidsniveau"`
		EmisDeeltjesType1Wltp          string `json:"emis_deeltjes_type1_wltp"`
		EmisCo2GecombineerdWltp        string `json:"emissie_co2_gecombineerd_wltp"`
		BrandstofVerbruikGecombineerd  string `json:"brandstof_verbruik_gecombineerd_wltp"`
	}

	// VehicleAxesInfo holds axle-level technical specifications.
	VehicleAxesInfo struct {
		Kenteken                         string `json:"kenteken"`
		AsNummer                         string `json:"as_nummer"`
		AantalAssen                      string `json:"aantal_assen"`
		AangedrevenAs                    string `json:"aangedreven_as"`
		PlaatscodeAs                     string `json:"plaatscode_as"`
		Spoorbreedte                     string `json:"spoorbreedte"`
		TechnischToegestaneMaximumAslast string `json:"technisch_toegestane_maximum_aslast"`
		WettelijkToegestaneMaximumAslast string `json:"wettelijk_toegestane_maximum_aslast"`
		AfstandTotVolgendeAsVoertuig     string `json:"afstand_tot_volgende_as_voertuig"`
	}

	// VehicleBodyInfo holds carrosserie (body type) classification data.
	VehicleBodyInfo struct {
		Kenteken                            string `json:"kenteken"`
		CarrosserieVolgnummer               string `json:"carrosserie_volgnummer"`
		Carrosserietype                     string `json:"carrosserietype"`
		TypeCarrosserieEuropeseOmschrijving string `json:"type_carrosserie_europese_omschrijving"`
	}

	// VehicleRecall holds an open RDW recall action for a vehicle.
	VehicleRecall struct {
		Kenteken          string `json:"kenteken"`
		ReferentiecodeRDW string `json:"referentiecode_rdw"`
		CodeStatus        string `json:"code_status"`
		Status            string `json:"status"`
		// Description is populated client-side by FetchRecalls from endpointRecallDesc.
		Description string `json:"omschrijving_defect,omitempty"`
		// PublicationDate is populated client-side by FetchRecalls.
		PublicationDate string `json:"publicatiedatum_rdw,omitempty"`
	}

	// recallDescription is the row schema for endpointRecallDesc, used to enrich VehicleRecall.
	recallDescription struct {
		ReferentiecodeRDW  string `json:"referentiecode_rdw"`
		OmschrijvingDefect string `json:"omschrijving_defect"`
		PublicatiedatumRDW string `json:"publicatiedatum_rdw"`
	}

	// VehicleAPKInspection holds one APK (MOT) inspection record for a vehicle.
	VehicleAPKInspection struct {
		Kenteken                       string `json:"kenteken"`
		SoortErkenningOmschrijving     string `json:"soort_erkenning_omschrijving"`
		SoortMeldingKIOmschrijving     string `json:"soort_melding_ki_omschrijving"`
		MeldDatumDoorKeuringsinstantie string `json:"meld_datum_door_keuringsinstantie"`
		MeldTijdDoorKeuringsinstantie  string `json:"meld_tijd_door_keuringsinstantie"`
		VervaldatumKeuring             string `json:"vervaldatum_keuring"`
	}

	// VehicleDefect holds one observed defect record for a vehicle.
	VehicleDefect struct {
		Kenteken                       string `json:"kenteken"`
		GebrekIdentificatie            string `json:"gebrek_identificatie"`
		AantalGebrekenGeconstateerd    string `json:"aantal_gebreken_geconstateerd"`
		MeldDatumDoorKeuringsinstantie string `json:"meld_datum_door_keuringsinstantie"`
		SoortErkenningOmschrijving     string `json:"soort_erkenning_omschrijving"`
	}

	// AllVehicleData bundles the results from all parallel RDW API calls.
	AllVehicleData struct {
		Base       *VehicleBaseInfo
		Fuel       []VehicleFuelInfo
		Axles      []VehicleAxesInfo
		Body       []VehicleBodyInfo
		Recalls    []VehicleRecall
		APKHistory []VehicleAPKInspection
		Defects    []VehicleDefect
	}
)

// FetchAllVehicleData runs every RDW endpoint call concurrently and returns
// the combined result. A nil Base means no vehicle was found for the kenteken.
func FetchAllVehicleData(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) (*AllVehicleData, error) {
	var (
		wg          sync.WaitGroup
		baseRecords []VehicleBaseInfo
		baseErr     error
		fuelRecords []VehicleFuelInfo
		fuelErr     error
		axleRecords []VehicleAxesInfo
		axleErr     error
		bodyRecords []VehicleBodyInfo
		bodyErr     error
		recallRows  []VehicleRecall
		recallErr   error
		apkRows     []VehicleAPKInspection
		apkErr      error
		defectRows  []VehicleDefect
		defectErr   error
	)

	wg.Add(7) //nolint:mnd // Seven parallel RDW endpoint calls.

	go func() {
		defer wg.Done()
		baseRecords, baseErr = makeRDWRequest[VehicleBaseInfo](ctx, client, endpointBase, kenteken)
	}()

	go func() {
		defer wg.Done()
		fuelRecords, fuelErr = makeRDWRequest[VehicleFuelInfo](ctx, client, endpointFuel, kenteken)
	}()

	go func() {
		defer wg.Done()
		axleRecords, axleErr = makeRDWRequest[VehicleAxesInfo](ctx, client, endpointAxles, kenteken)
	}()

	go func() {
		defer wg.Done()
		bodyRecords, bodyErr = makeRDWRequest[VehicleBodyInfo](ctx, client, endpointBody, kenteken)
	}()

	go func() {
		defer wg.Done()
		recallRows, recallErr = FetchRecalls(ctx, client, kenteken)
	}()

	go func() {
		defer wg.Done()
		apkRows, apkErr = makeRDWRequest[VehicleAPKInspection](ctx, client, endpointAPK, kenteken)
	}()

	go func() {
		defer wg.Done()
		defectRows, defectErr = makeRDWRequest[VehicleDefect](
			ctx,
			client,
			endpointDefects,
			kenteken,
		)
	}()

	wg.Wait()

	if baseErr != nil {
		return nil, fmt.Errorf("base endpoint: %w", baseErr)
	}

	if len(baseRecords) == 0 {
		return &AllVehicleData{}, nil
	}

	data := buildResult(
		baseRecords[0],
		fuelRecords,
		fuelErr,
		axleRecords,
		axleErr,
		bodyRecords,
		bodyErr,
	)

	if recallErr == nil {
		data.Recalls = recallRows
	}

	if apkErr == nil {
		data.APKHistory = apkRows
	}

	if defectErr == nil {
		data.Defects = defectRows
	}

	return data, nil
}

// FetchBase fetches the primary registration record for a kenteken. Returns
// (nil, nil) when no vehicle is found.
func FetchBase(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) (*VehicleBaseInfo, error) {
	rows, err := makeRDWRequest[VehicleBaseInfo](ctx, client, endpointBase, kenteken)
	if err != nil {
		return nil, fmt.Errorf("base endpoint: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil //nolint:nilnil // explicit "not found" sentinel
	}

	return &rows[0], nil
}

// FetchFuel returns all fuel/emissions records for a kenteken.
func FetchFuel(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleFuelInfo, error) {
	return makeRDWRequest[VehicleFuelInfo](ctx, client, endpointFuel, kenteken)
}

// FetchAxles returns all axle specification records for a kenteken.
func FetchAxles(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleAxesInfo, error) {
	return makeRDWRequest[VehicleAxesInfo](ctx, client, endpointAxles, kenteken)
}

// FetchBody returns all carrosserie records for a kenteken.
func FetchBody(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleBodyInfo, error) {
	return makeRDWRequest[VehicleBodyInfo](ctx, client, endpointBody, kenteken)
}

// FetchAPKHistory returns all APK (MOT) inspection records for a kenteken.
func FetchAPKHistory(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleAPKInspection, error) {
	return makeRDWRequest[VehicleAPKInspection](ctx, client, endpointAPK, kenteken)
}

// FetchDefects returns all observed defect records for a kenteken.
func FetchDefects(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleDefect, error) {
	return makeRDWRequest[VehicleDefect](ctx, client, endpointDefects, kenteken)
}

// FetchRecalls returns all open recall actions for a kenteken, enriched with
// the human-readable description and publication date from the recall catalog.
func FetchRecalls(
	ctx context.Context,
	client *http.Client,
	kenteken string,
) ([]VehicleRecall, error) {
	recalls, err := makeRDWRequest[VehicleRecall](ctx, client, endpointRecalls, kenteken)
	if err != nil {
		return nil, fmt.Errorf("recalls endpoint: %w", err)
	}

	for idx := range recalls {
		ref := recalls[idx].ReferentiecodeRDW
		if ref == "" {
			continue
		}

		desc, descErr := lookupRecallDescription(ctx, client, ref)
		if descErr != nil || desc == nil {
			continue
		}

		recalls[idx].Description = desc.OmschrijvingDefect
		recalls[idx].PublicationDate = desc.PublicatiedatumRDW
	}

	return recalls, nil
}

// lookupRecallDescription queries the recall description catalog (j9yg-7rg9) for
// a single referentiecode_rdw. Returns (nil, nil) when no entry is found.
func lookupRecallDescription(
	ctx context.Context,
	client *http.Client,
	referentiecode string,
) (*recallDescription, error) {
	rawURL := fmt.Sprintf("%s/%s.json", APIBase, endpointRecallDesc)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	query := parsed.Query()
	query.Set("referentiecode_rdw", referentiecode)
	parsed.RawQuery = query.Encode()

	rows, err := doRDWGet[recallDescription](ctx, client, endpointRecallDesc, parsed.String())
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil //nolint:nilnil // explicit "not found" sentinel
	}

	return &rows[0], nil
}

// buildResult constructs an AllVehicleData from individual endpoint results.
func buildResult(
	base VehicleBaseInfo,
	fuelRecords []VehicleFuelInfo, fuelErr error,
	axleRecords []VehicleAxesInfo, axleErr error,
	bodyRecords []VehicleBodyInfo, bodyErr error,
) *AllVehicleData {
	// Use power from fuel data for consistency when available.
	if fuelErr == nil && len(fuelRecords) > 0 && fuelRecords[0].Nettomaximumvermogen != "" {
		base.Nettomaximumvermogen = fuelRecords[0].Nettomaximumvermogen
	}

	var fuel []VehicleFuelInfo
	if fuelErr == nil {
		fuel = fuelRecords
	}

	var axles []VehicleAxesInfo
	if axleErr == nil {
		axles = axleRecords
	}

	var body []VehicleBodyInfo
	if bodyErr == nil {
		body = bodyRecords
	}

	return &AllVehicleData{
		Base:  &base,
		Fuel:  fuel,
		Axles: axles,
		Body:  body,
	}
}

// makeRDWRequest fetches a JSON array from the given RDW endpoint filtered by kenteken.
func makeRDWRequest[T any](
	ctx context.Context,
	client *http.Client,
	endpoint, kenteken string,
) ([]T, error) {
	rawURL := fmt.Sprintf("%s/%s.json", APIBase, endpoint)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	query := parsed.Query()
	query.Set("kenteken", kenteken)
	parsed.RawQuery = query.Encode()

	return doRDWGet[T](ctx, client, endpoint, parsed.String())
}

// doRDWGet performs a GET request against the given fully-qualified Socrata URL
// and decodes the JSON response into a slice of T. It transparently applies
// the active per-call timeout, exponential-backoff retry on transient errors,
// and an in-memory TTL LRU cache keyed by the full URL.
//
// dataset is the RDW dataset identifier; it drives the rdw.endpoint telemetry
// label and is collapsed via endpointName so cardinality stays bounded.
//
//nolint:nonamedreturns // intentional: the deferred recorder reads result/err after the body returns
func doRDWGet[T any](
	ctx context.Context,
	client *http.Client,
	dataset, fullURL string,
) (result []T, err error) {
	cfg := getConfig()
	cache := getCache()

	tracer, reqDuration, retryHist, cacheOps := instruments()
	label := endpointName(dataset)

	ctx, span := tracer.Start(ctx, "rdw "+label, trace.WithAttributes(
		attribute.String("rdw.endpoint", label),
	))
	defer span.End()

	start := time.Now()
	attempts := 0
	cacheHit := false

	// Deferred recorder per logical call. A panic still observes a sample
	// tagged "internal" so the rdw.* signals stay reliable. The dedicated
	// defer span.End() above runs after this and survives the re-panic.
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr := fmt.Errorf("panic: %v", recovered)
			span.RecordError(panicErr,
				trace.WithAttributes(attribute.String("error.type", "internal")))
			span.SetAttributes(attribute.String("error.type", "internal"))
			span.SetStatus(codes.Error, "panic")
			err = panicErr

			span.SetAttributes(
				attribute.Bool("rdw.cache.hit", cacheHit),
				attribute.Int("rdw.attempts", attempts),
			)
			recordRDWCall(ctx, reqDuration, retryHist, label, err, attempts, time.Since(start))

			panic(recovered)
		}

		span.SetAttributes(
			attribute.Bool("rdw.cache.hit", cacheHit),
			attribute.Int("rdw.attempts", attempts),
		)
		recordRDWCall(ctx, reqDuration, retryHist, label, err, attempts, time.Since(start))
	}()

	if cache != nil {
		if raw, ok := cache.get(fullURL); ok {
			cacheHit = true
			recordCacheLookup(ctx, cacheOps, label, true)
			result, err = decodeRDWBody[T](raw)
			return result, err
		}
		recordCacheLookup(ctx, cacheOps, label, false)
	}

	body, runErr := runRetryLoop(ctx, client, span, cfg, fullURL, &attempts)
	if runErr != nil {
		err = runErr
		return nil, runErr
	}

	if cache != nil {
		cache.set(fullURL, body, cfg.CacheTTL)
	}

	result, err = decodeRDWBody[T](body)
	return result, err
}

// runRetryLoop drives the per-attempt retry/backoff sequence and returns the
// successful body or the terminating error. It writes attempt count into the
// caller's counter so the deferred recorder in doRDWGet sees the final value.
func runRetryLoop(
	ctx context.Context,
	client *http.Client,
	span trace.Span,
	cfg ClientConfig,
	fullURL string,
	attempts *int,
) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, cfg.PerCallTimeout)
	defer cancel()

	var (
		body    []byte
		lastErr error
	)

	for attempt := range cfg.MaxAttempts {
		*attempts = attempt + 1
		body, lastErr = singleRDWGet(callCtx, client, fullURL)
		if lastErr == nil {
			return body, nil
		}

		// A bounded "retry" event per attempt lets traces show retry pacing
		// without spawning a span per attempt (otelhttp already covers those).
		span.AddEvent("rdw.retry", trace.WithAttributes(
			attribute.Int("attempt", *attempts),
			attribute.String("error.type", classifyRDWError(lastErr)),
		))

		if !errors.Is(lastErr, errRetryable) || attempt == cfg.MaxAttempts-1 {
			span.RecordError(lastErr,
				trace.WithAttributes(attribute.String("error.type", classifyRDWError(lastErr))))
			span.SetStatus(codes.Error, lastErr.Error())
			return nil, lastErr
		}

		backoff := computeBackoff(attempt, cfg.BaseBackoff, cfg.MaxBackoff)
		if sleepErr := sleepCtx(callCtx, backoff); sleepErr != nil {
			span.RecordError(sleepErr,
				trace.WithAttributes(attribute.String("error.type", classifyRDWError(sleepErr))))
			span.SetStatus(codes.Error, sleepErr.Error())
			return nil, sleepErr
		}
	}

	return body, lastErr
}

// recordRDWCall observes the duration histogram and the retry-attempts histogram
// for one logical doRDWGet call. errType is "" on success, in which case the
// label is omitted (low-cardinality contract).
func recordRDWCall(
	ctx context.Context,
	reqDuration metric.Float64Histogram,
	retryHist metric.Int64Histogram,
	endpoint string,
	err error,
	attempts int,
	elapsed time.Duration,
) {
	durAttrs := []attribute.KeyValue{
		attribute.String("rdw.endpoint", endpoint),
		attribute.String("rdw.status", callStatus(err)),
	}
	if errType := classifyRDWError(err); errType != "" {
		durAttrs = append(durAttrs, attribute.String("error.type", errType))
	}
	reqDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(durAttrs...))

	// attempts can be 0 only when the cache hit short-circuited before the
	// retry loop ran — don't record a "0 attempts" sample in that case.
	if attempts > 0 {
		retryHist.Record(ctx, int64(attempts), metric.WithAttributes(
			attribute.String("rdw.endpoint", endpoint),
		))
	}
}

// singleRDWGet performs one GET request and returns the raw response body.
// Transient failures (5xx, 408, 429, network errors) are returned wrapped
// with errRetryable so the caller may retry.
func singleRDWGet(ctx context.Context, client *http.Client, fullURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("http get: %w", err)
		}

		return nil, fmt.Errorf("%w: http get: %w", errRetryable, err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to close response body", "err", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		if isRetryableStatus(resp.StatusCode) {
			return nil, fmt.Errorf("%w: unexpected status %d", errRetryable, resp.StatusCode)
		}

		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %w", errRetryable, err)
	}

	return body, nil
}

// decodeRDWBody unmarshals a raw JSON array body into []T.
func decodeRDWBody[T any](body []byte) ([]T, error) {
	var result []T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return result, nil
}
