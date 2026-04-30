package rdw_test

// End-to-end tests using verbatim JSON fixtures captured from the live RDW
// open data API. The fixtures are served by a local mock transport so the
// tests are hermetic and fast, but the payload is identical to what the real
// API returns.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

// ---------------------------------------------------------------------------
// Real JSON fixtures (captured from opendata.rdw.nl, April 2026)
// ---------------------------------------------------------------------------

// F794KP — 2012 Yamaha Aerox (bromfiets, 49 cc, 2 kW).
const (
	fixtureF794KPBase = `[{"kenteken":"F794KP","voertuigsoort":"Bromfiets","merk":"YAMAHA","handelsbenaming":"AEROX","datum_tenaamstelling":"20201219","inrichting":"N.v.t.","aantal_zitplaatsen":"2","eerste_kleur":"N.v.t.","tweede_kleur":"N.v.t.","aantal_cilinders":"1","cilinderinhoud":"49","massa_ledig_voertuig":"92","massa_rijklaar":"97","datum_eerste_toelating":"20120823","datum_eerste_tenaamstelling_in_nederland":"20120823","wacht_op_keuren":"Geen verstrekking in Open Data","wam_verzekerd":"Ja","maximale_constructiesnelheid":"45","aantal_deuren":"0","aantal_wielen":"2","europese_voertuigcategorie":"L1","europese_voertuigcategorie_toevoeging":"e","technische_max_massa_voertuig":"277","typegoedkeuringsnummer":"e13*92/61*0036*06","variant":"I","uitvoering":"III","export_indicator":"Nee","openstaande_terugroepactie_indicator":"Nee","taxi_indicator":"Nee","tellerstandoordeel":"Niet geregistreerd","code_toelichting_tellerstandoordeel":"NG"}]`
	fixtureF794KPFuel = `[{"kenteken":"F794KP","brandstof_volgnummer":"1","brandstof_omschrijving":"Benzine","geluidsniveau_stationair":"80","nettomaximumvermogen":"2.00","toerental_geluidsniveau":"3250"}]`
)

// TT601D — 2013 Volkswagen Kombi (personenauto, 2.0 TDI diesel, MPV).
const (
	fixtureTT601DBase  = `[{"kenteken":"TT601D","voertuigsoort":"Personenauto","merk":"VOLKSWAGEN","handelsbenaming":"KOMBI","vervaldatum_apk":"20270517","datum_tenaamstelling":"20130725","bruto_bpm":"27561","inrichting":"MPV","aantal_zitplaatsen":"9","eerste_kleur":"ROOD","tweede_kleur":"Niet geregistreerd","aantal_cilinders":"4","cilinderinhoud":"1968","massa_ledig_voertuig":"1876","toegestane_maximum_massa_voertuig":"3200","massa_rijklaar":"1976","maximum_massa_trekken_ongeremd":"750","maximum_trekken_massa_geremd":"2500","datum_eerste_toelating":"20130517","datum_eerste_tenaamstelling_in_nederland":"20130517","catalogusprijs":"71828","wam_verzekerd":"Ja","aantal_wielen":"4","europese_voertuigcategorie":"M1","technische_max_massa_voertuig":"3200","type":"7HC","typegoedkeuringsnummer":"e1*2001/116*0220*31","export_indicator":"Nee","openstaande_terugroepactie_indicator":"Nee","taxi_indicator":"Nee","jaar_laatste_registratie_tellerstand":"2026","tellerstandoordeel":"Logisch","code_toelichting_tellerstandoordeel":"00","zuinigheidsclassificatie":"D"}]`
	fixtureTT601DFuel  = `[{"kenteken":"TT601D","brandstof_volgnummer":"1","brandstof_omschrijving":"Diesel","brandstofverbruik_buiten":"6.70","brandstofverbruik_gecombineerd":"8.00","brandstofverbruik_stad":"10.20","co2_uitstoot_gecombineerd":"211","geluidsniveau_stationair":"74","emissiecode_omschrijving":"5","milieuklasse_eg_goedkeuring_licht":"715/2007*566/2011G","uitstoot_deeltjes_licht":"0.00039","nettomaximumvermogen":"103.00","roetuitstoot":"0.60","toerental_geluidsniveau":"2375","uitlaatemissieniveau":"EURO 5 G"}]`
	fixtureTT601DAxles = `[{"kenteken":"TT601D","as_nummer":"1","aantal_assen":"2","spoorbreedte":"163","wettelijk_toegestane_maximum_aslast":"1675","technisch_toegestane_maximum_aslast":"1675"},{"kenteken":"TT601D","as_nummer":"2","aantal_assen":"2","spoorbreedte":"163","wettelijk_toegestane_maximum_aslast":"1680","technisch_toegestane_maximum_aslast":"1680"}]`
	fixtureTT601DBody  = `[{"kenteken":"TT601D","carrosserie_volgnummer":"1","carrosserietype":"AF","type_carrosserie_europese_omschrijving":"Multipurpose vehicle (MPV)"}]`
	fixtureTT601DAPK   = `[{"kenteken":"TT601D","soort_erkenning_keuringsinstantie":"AL","meld_datum_door_keuringsinstantie":"20260319","meld_tijd_door_keuringsinstantie":"1515","soort_erkenning_omschrijving":"APK Lichte voertuigen","soort_melding_ki_omschrijving":"periodieke controle","vervaldatum_keuring":"20270517"},{"kenteken":"TT601D","soort_erkenning_keuringsinstantie":"AL","meld_datum_door_keuringsinstantie":"20240328","meld_tijd_door_keuringsinstantie":"1117","soort_erkenning_omschrijving":"APK Lichte voertuigen","soort_melding_ki_omschrijving":"periodieke controle","vervaldatum_keuring":"20250517"}]`
)

// jsonHandler returns an HTTP handler that writes the given JSON body.
func jsonHandler(t *testing.T, body string) http.HandlerFunc {
	t.Helper()

	return func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_, err := rw.Write([]byte(body))
		assert.NoError(t, err)
	}
}

// ---------------------------------------------------------------------------
// F794KP — Yamaha Aerox bromfiets
// ---------------------------------------------------------------------------

func TestE2E_F794KP_FetchAllVehicleData(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: endpointTransport{
			handlers: map[string]http.HandlerFunc{
				epBase:  jsonHandler(t, fixtureF794KPBase),
				epFuel:  jsonHandler(t, fixtureF794KPFuel),
				epAxles: emptyJSONArray(t),
				epBody:  emptyJSONArray(t),
				// recalls, APK and defects are all empty for this plate
				"t49b-isb7": emptyJSONArray(t),
				"sgfe-77wx": emptyJSONArray(t),
				"a34c-vvps": emptyJSONArray(t),
			},
		},
	}

	data, err := rdw.FetchAllVehicleData(context.Background(), client, "F794KP")
	require.NoError(t, err)
	require.NotNil(t, data.Base)

	t.Run("base fields", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "F794KP", data.Base.Kenteken)
		assert.Equal(t, "YAMAHA", data.Base.Merk)
		assert.Equal(t, "AEROX", data.Base.Handelsbenaming)
		assert.Equal(t, "Bromfiets", data.Base.Voertuigsoort)
		assert.Equal(t, "49", data.Base.Cilinderinhoud)
		assert.Equal(t, "1", data.Base.AantalCilinders)
		assert.Equal(t, "20120823", data.Base.DatumEersteToelating)
		assert.Equal(t, "Ja", data.Base.WamVerzekerd)
		assert.Equal(t, "Nee", data.Base.ExportIndicator)
		assert.Equal(t, "Nee", data.Base.TaxiIndicator)
	})

	t.Run("fuel merged into base", func(t *testing.T) {
		t.Parallel()
		// nettomaximumvermogen from fuel is merged into base
		assert.Equal(t, "2.00", data.Base.Nettomaximumvermogen)
	})

	t.Run("fuel data", func(t *testing.T) {
		t.Parallel()
		require.Len(t, data.Fuel, 1)
		assert.Equal(t, "Benzine", data.Fuel[0].BrandstofOmschrijving)
		assert.Equal(t, "80", data.Fuel[0].GeluidsniveauStationair)
	})

	t.Run("no axles or body for bromfiets", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, data.Axles)
		assert.Empty(t, data.Body)
	})

	t.Run("no recalls APK or defects", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, data.Recalls)
		assert.Empty(t, data.APKHistory)
		assert.Empty(t, data.Defects)
	})
}

func TestE2E_F794KP_FormatOutput(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: endpointTransport{
			handlers: map[string]http.HandlerFunc{
				epBase:      jsonHandler(t, fixtureF794KPBase),
				epFuel:      jsonHandler(t, fixtureF794KPFuel),
				epAxles:     emptyJSONArray(t),
				epBody:      emptyJSONArray(t),
				"t49b-isb7": emptyJSONArray(t),
				"sgfe-77wx": emptyJSONArray(t),
				"a34c-vvps": emptyJSONArray(t),
			},
		},
	}

	data, err := rdw.FetchAllVehicleData(context.Background(), client, "F794KP")
	require.NoError(t, err)

	output := rdw.FormatVehicleInfo(data)

	assert.Contains(t, output, "YAMAHA")
	assert.Contains(t, output, "AEROX")
	assert.Contains(t, output, "Bromfiets")
	assert.Contains(t, output, "Benzine")
	assert.Contains(t, output, "49")
}

// ---------------------------------------------------------------------------
// TT601D — Volkswagen Kombi MPV (full data: fuel, axles, body, APK history)
// ---------------------------------------------------------------------------

func TestE2E_TT601D_FetchAllVehicleData(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: endpointTransport{
			handlers: map[string]http.HandlerFunc{
				epBase:      jsonHandler(t, fixtureTT601DBase),
				epFuel:      jsonHandler(t, fixtureTT601DFuel),
				epAxles:     jsonHandler(t, fixtureTT601DAxles),
				epBody:      jsonHandler(t, fixtureTT601DBody),
				"t49b-isb7": emptyJSONArray(t),
				"sgfe-77wx": jsonHandler(t, fixtureTT601DAPK),
				"a34c-vvps": emptyJSONArray(t),
			},
		},
	}

	data, err := rdw.FetchAllVehicleData(context.Background(), client, "TT601D")
	require.NoError(t, err)
	require.NotNil(t, data.Base)

	t.Run("base fields", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "TT601D", data.Base.Kenteken)
		assert.Equal(t, "VOLKSWAGEN", data.Base.Merk)
		assert.Equal(t, "KOMBI", data.Base.Handelsbenaming)
		assert.Equal(t, "Personenauto", data.Base.Voertuigsoort)
		assert.Equal(t, "20270517", data.Base.VervaldatumApk)
		assert.Equal(t, "1968", data.Base.Cilinderinhoud)
		assert.Equal(t, "4", data.Base.AantalCilinders)
		assert.Equal(t, "71828", data.Base.Catalogusprijs)
		assert.Equal(t, "27561", data.Base.BrutoBpm)
		assert.Equal(t, "1976", data.Base.MassaRijklaar)
		assert.Equal(t, "ROOD", data.Base.EersteKleur)
		assert.Equal(t, "Logisch", data.Base.Tellerstandoordeel)
		assert.Equal(t, "2026", data.Base.JaarLaatsteRegistratieTellerstand)
	})

	t.Run("fuel merged into base", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "103.00", data.Base.Nettomaximumvermogen)
	})

	t.Run("fuel data", func(t *testing.T) {
		t.Parallel()
		require.Len(t, data.Fuel, 1)
		assert.Equal(t, "Diesel", data.Fuel[0].BrandstofOmschrijving)
		assert.Equal(t, "5", data.Fuel[0].EmissiecodeOmschrijving)
		assert.Equal(t, "EURO 5 G", data.Fuel[0].Uitlaatemissieniveau)
		assert.Equal(t, "0.60", data.Fuel[0].Roetuitstoot)
	})

	t.Run("two axles", func(t *testing.T) {
		t.Parallel()
		require.Len(t, data.Axles, 2)
		assert.Equal(t, "1", data.Axles[0].AsNummer)
		assert.Equal(t, "2", data.Axles[0].AantalAssen)
		assert.Equal(t, "163", data.Axles[0].Spoorbreedte)
		assert.Equal(t, "1675", data.Axles[0].WettelijkToegestaneMaximumAslast)
		assert.Equal(t, "2", data.Axles[1].AsNummer)
		assert.Equal(t, "1680", data.Axles[1].WettelijkToegestaneMaximumAslast)
	})

	t.Run("body MPV", func(t *testing.T) {
		t.Parallel()
		require.Len(t, data.Body, 1)
		assert.Equal(t, "AF", data.Body[0].Carrosserietype)
		assert.Equal(
			t,
			"Multipurpose vehicle (MPV)",
			data.Body[0].TypeCarrosserieEuropeseOmschrijving,
		)
	})

	t.Run("APK history", func(t *testing.T) {
		t.Parallel()
		require.Len(t, data.APKHistory, 2)
		assert.Equal(t, "20270517", data.APKHistory[0].VervaldatumKeuring)
		assert.Equal(t, "periodieke controle", data.APKHistory[0].SoortMeldingKIOmschrijving)
		assert.Equal(t, "20250517", data.APKHistory[1].VervaldatumKeuring)
	})

	t.Run("no recalls or defects", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, data.Recalls)
		assert.Empty(t, data.Defects)
	})
}

func TestE2E_TT601D_FormatOutput(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: endpointTransport{
			handlers: map[string]http.HandlerFunc{
				epBase:      jsonHandler(t, fixtureTT601DBase),
				epFuel:      jsonHandler(t, fixtureTT601DFuel),
				epAxles:     jsonHandler(t, fixtureTT601DAxles),
				epBody:      jsonHandler(t, fixtureTT601DBody),
				"t49b-isb7": emptyJSONArray(t),
				"sgfe-77wx": jsonHandler(t, fixtureTT601DAPK),
				"a34c-vvps": emptyJSONArray(t),
			},
		},
	}

	data, err := rdw.FetchAllVehicleData(context.Background(), client, "TT601D")
	require.NoError(t, err)

	output := rdw.FormatVehicleInfo(data)

	assert.Contains(t, output, "VOLKSWAGEN")
	assert.Contains(t, output, "KOMBI")
	assert.Contains(t, output, "Diesel")
	assert.Contains(t, output, "Multipurpose vehicle (MPV)")
	assert.Contains(t, output, "ROOD")
}
