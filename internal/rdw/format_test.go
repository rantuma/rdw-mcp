package rdw_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rantuma/rdw-mcp/internal/rdw"
)

func newFullData() *rdw.AllVehicleData {
	base := rdw.VehicleBaseInfo{
		Kenteken:                           "F794KP",
		Voertuigsoort:                      "Personenauto",
		Merk:                               "TOYOTA",
		Handelsbenaming:                    "Yaris",
		EersteKleur:                        "Blauw",
		TweedeKleur:                        "Zwart",
		AantalZitplaatsen:                  "5",
		DatumEersteToelating:               "20150601",
		VervaldatumApk:                     "20261201",
		Inrichting:                         "Hatchback",
		AantalDeuren:                       "5",
		MassaLedigVoertuig:                 "1050",
		MassaRijklaar:                      "1100",
		MaximumMassaTrekkenGeremd:          "1200",
		MaximumMassaTrekkenOngeremd:        "640",
		BrutoBpm:                           "2500",
		Catalogusprijs:                     "22000",
		Nettomaximumvermogen:               "85",
		AantalCilinders:                    "3",
		Cilinderinhoud:                     "998",
		Lengte:                             "3995",
		Breedte:                            "1695",
		WamVerzekerd:                       "Ja",
		ExportIndicator:                    "Nee",
		OpenstaandeTerugroepactieIndicator: "Nee",
		TaxiIndicator:                      "Nee",
		Tellerstandoordeel:                 "Logisch",
		JaarLaatsteRegistratieTellerstand:  "2024",
	}

	fuel := []rdw.VehicleFuelInfo{
		{
			Kenteken:                "F794KP",
			BrandstofOmschrijving:   "Benzine",
			Nettomaximumvermogen:    "85",
			EmissiecodeOmschrijving: "E6",
			Co2Emissieklasse:        "A",
		},
	}

	axles := []rdw.VehicleAxesInfo{
		{
			Kenteken:      "F794KP",
			AsNummer:      "1",
			AantalAssen:   "2",
			AangedrevenAs: "J",
		},
	}

	body := []rdw.VehicleBodyInfo{
		{
			Kenteken:                            "F794KP",
			Carrosserietype:                     "Hatchback",
			TypeCarrosserieEuropeseOmschrijving: "AB",
		},
	}

	return &rdw.AllVehicleData{
		Base:  &base,
		Fuel:  fuel,
		Axles: axles,
		Body:  body,
	}
}

func TestFormatVehicleInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		buildData    func() *rdw.AllVehicleData
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:      "full data produces all section headers",
			buildData: newFullData,
			wantContains: []string{
				"BASIC INFORMATION:",
				"APPEARANCE:",
				"CAPACITY:",
				"TECHNICAL SPECIFICATIONS:",
				"WEIGHTS & TOWING CAPACITY:",
				"REGISTRATION:",
				"INSPECTION:",
				"STATUS INDICATORS:",
				"FUEL & EMISSIONS:",
				"AXLE SPECIFICATIONS:",
				"BODY SPECIFICATIONS:",
				"TOYOTA",
				"Yaris",
				"Benzine",
				"Last Odometer Reading: 2024",
			},
		},
		{
			name: "no fuel data omits fuel section",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Fuel = nil

				return data
			},
			wantAbsent: []string{"FUEL & EMISSIONS:"},
			wantContains: []string{
				"BASIC INFORMATION:",
				"TOYOTA",
			},
		},
		{
			name: "no axles data omits axle section",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Axles = nil

				return data
			},
			wantAbsent: []string{"AXLE SPECIFICATIONS:"},
		},
		{
			name: "no body data omits body section",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Body = nil

				return data
			},
			wantAbsent: []string{"BODY SPECIFICATIONS:"},
		},
		{
			name: "secondary color not registered is excluded",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Base.TweedeKleur = "Niet geregistreerd"

				return data
			},
			wantAbsent: []string{"Secondary Color"},
		},
		{
			name: "secondary color registered is included",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Base.TweedeKleur = "Rood"

				return data
			},
			wantContains: []string{"Secondary Color"},
		},
		{
			name: "no financial data omits financial section",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Base.BrutoBpm = ""
				data.Base.Catalogusprijs = ""

				return data
			},
			wantAbsent: []string{"FINANCIAL:"},
		},
		{
			name: "financial data present includes financial section",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Base.BrutoBpm = "2500"
				data.Base.Catalogusprijs = "22000"

				return data
			},
			wantContains: []string{"FINANCIAL:"},
		},
		{
			name: "empty base fields show Unknown",
			buildData: func() *rdw.AllVehicleData {
				return &rdw.AllVehicleData{
					Base: &rdw.VehicleBaseInfo{Kenteken: "9KHX31"},
				}
			},
			wantContains: []string{
				"BASIC INFORMATION:",
				"Unknown",
				"Last Odometer Reading: Unknown",
			},
		},
		{
			name: "driven axle shows Ja label",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Axles = []rdw.VehicleAxesInfo{{AsNummer: "1", AangedrevenAs: "J"}}

				return data
			},
			wantContains: []string{"Driven: Yes"},
		},
		{
			name: "non-driven axle shows Nee label",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Axles = []rdw.VehicleAxesInfo{{AsNummer: "2", AangedrevenAs: "N"}}

				return data
			},
			wantContains: []string{"Driven: No"},
		},
		{
			name: "unknown driven status shows Unknown",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Axles = []rdw.VehicleAxesInfo{{AsNummer: "3", AangedrevenAs: ""}}

				return data
			},
			wantContains: []string{"Driven: Unknown"},
		},
		{
			name: "multiple axles shows total count",
			buildData: func() *rdw.AllVehicleData {
				data := newFullData()
				data.Axles = []rdw.VehicleAxesInfo{
					{AsNummer: "1", AantalAssen: "4"},
					{AsNummer: "2", AantalAssen: "4"},
				}

				return data
			},
			wantContains: []string{"Total Axles: 4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := tc.buildData()
			result := rdw.FormatVehicleInfo(data)

			require.NotEmpty(t, result)

			for _, want := range tc.wantContains {
				assert.Contains(t, result, want,
					"expected output to contain %q\ngot:\n%s", want, result)
			}

			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, result, absent,
					"expected output NOT to contain %q\ngot:\n%s", absent, result)
			}
		})
	}
}
