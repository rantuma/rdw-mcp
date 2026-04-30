package rdw

import (
	"fmt"
	"strconv"
	"strings"
)

const unknown = "Unknown"

// FormatVehicleInfo assembles the full multi-section text report for a vehicle.
func FormatVehicleInfo(data *AllVehicleData) string {
	base := data.Base

	var sb strings.Builder

	writeSection(&sb, "BASIC INFORMATION", formatBasic(base))
	writeSection(&sb, "APPEARANCE", formatAppearance(base))
	writeSection(&sb, "CAPACITY", formatCapacity(base))
	writeSection(&sb, "TECHNICAL SPECIFICATIONS", formatTechnical(base))
	writeSection(&sb, "WEIGHTS & TOWING CAPACITY", formatMasses(base))
	writeSection(&sb, "REGISTRATION", formatRegistration(base))
	writeSection(&sb, "INSPECTION", formatInspection(base))

	if fin := formatFinancial(base); fin != "" {
		writeSection(&sb, "FINANCIAL", fin)
	}

	writeSection(&sb, "STATUS INDICATORS", formatIndicators(base))

	if len(data.Fuel) > 0 {
		writeSection(&sb, "FUEL & EMISSIONS", formatFuel(data.Fuel))
	}

	if len(data.Axles) > 0 {
		writeSection(&sb, "AXLE SPECIFICATIONS", formatAxles(data.Axles))
	}

	if len(data.Body) > 0 {
		writeSection(&sb, "BODY SPECIFICATIONS", formatBody(data.Body))
	}

	if len(data.Recalls) > 0 {
		writeSection(&sb, "OPEN RECALLS", formatRecalls(data.Recalls))
	}

	if len(data.APKHistory) > 0 {
		writeSection(&sb, "APK INSPECTION HISTORY", formatAPKHistory(data.APKHistory))
	}

	if len(data.Defects) > 0 {
		writeSection(&sb, "REPORTED DEFECTS", formatDefects(data.Defects))
	}

	sb.WriteString(
		"Last Odometer Reading: " + orUnknown(base.JaarLaatsteRegistratieTellerstand),
	)

	return sb.String()
}

// writeSection appends a named section with its content to sb.
func writeSection(sb *strings.Builder, title, content string) {
	sb.WriteString(title)
	sb.WriteString(":\n")
	sb.WriteString(content)
	sb.WriteString("\n\n")
}

// orUnknown returns s when non-empty, otherwise "Unknown".
func orUnknown(s string) string {
	if s == "" {
		return unknown
	}

	return s
}

// orFirst returns the first non-empty value from candidates, or "Unknown".
func orFirst(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}

	return unknown
}

// formatMeasure builds "Label: <value> <unit>" or "Label: Unknown" when value is empty.
func formatMeasure(label, value, unit string) string {
	if value == "" {
		return label + ": Unknown"
	}

	return label + ": " + value + " " + unit
}

// withUnit returns "<value> <unit>" or "Unknown" when value is already "Unknown".
func withUnit(value, unit string) string {
	if value == unknown {
		return unknown
	}

	return value + " " + unit
}

func formatBasic(base *VehicleBaseInfo) string {
	lines := []string{
		"License Plate: " + orUnknown(base.Kenteken),
		"Vehicle Type: " + orUnknown(base.Voertuigsoort),
		"Brand: " + orUnknown(base.Merk),
		"Model: " + orUnknown(base.Handelsbenaming),
		"Variant: " + orUnknown(base.Variant),
		"Version: " + orUnknown(base.Uitvoering),
		"European Category: " + orUnknown(base.EuropeseVoertuigcategorie),
		"Vehicle Type Code: " + orUnknown(base.Type),
	}

	return strings.Join(lines, "\n")
}

func formatAppearance(base *VehicleBaseInfo) string {
	lines := []string{
		"Primary Color: " + orUnknown(base.EersteKleur),
	}

	if base.TweedeKleur != "" && base.TweedeKleur != "Niet geregistreerd" {
		lines = append(lines, "Secondary Color: "+base.TweedeKleur)
	}

	lines = append(lines,
		"Body Type: "+orUnknown(base.Inrichting),
		"Number of Doors: "+orUnknown(base.AantalDeuren),
		"Number of Wheels: "+orUnknown(base.AantalWielen),
		formatMeasure("Length", base.Lengte, "cm"),
		formatMeasure("Width", base.Breedte, "cm"),
		formatMeasure("Height", base.HoogteVoertuig, "cm"),
		formatMeasure("Wheelbase", base.Wielbasis, "cm"),
	)

	return strings.Join(lines, "\n")
}

func formatCapacity(base *VehicleBaseInfo) string {
	lines := []string{
		"Seats: " + orUnknown(base.AantalZitplaatsen),
	}

	if base.AantalStaanplaatsen != "" {
		lines = append(lines, "Standing Places: "+base.AantalStaanplaatsen)
	}

	return strings.Join(lines, "\n")
}

func formatTechnical(base *VehicleBaseInfo) string {
	lines := []string{
		"Engine Cylinders: " + orUnknown(base.AantalCilinders),
		formatMeasure("Engine Displacement", base.Cilinderinhoud, "cc"),
		formatMeasure("Net Max Power", base.Nettomaximumvermogen, "kW"),
		formatMeasure("Power/Mass Ratio", base.VermogenMassarijklaar, "kW/kg"),
		formatMeasure("Max Construction Speed", base.MaximaleConstructiesnelheid, "km/h"),
	}

	if base.NominaalContinuMaximumvermogen != "" {
		lines = append(
			lines,
			"Nominal Continuous Max Power: "+base.NominaalContinuMaximumvermogen+" kW",
		)
	}

	return strings.Join(lines, "\n")
}

func formatMasses(base *VehicleBaseInfo) string {
	towingBraked := orFirst(base.MaximumTrekkenMassaGeremd, base.MaximumMassaTrekkenGeremd)
	maxMass := orFirst(base.ToegestaneMaximumMassaVoertuig, base.MaximumMassaVoertuig)

	lines := []string{
		formatMeasure("Empty Weight (Massa ledig)", base.MassaLedigVoertuig, "kg"),
		formatMeasure("Curb Weight (Massa rijklaar)", base.MassaRijklaar, "kg"),
		"Maximum Vehicle Mass: " + withUnit(maxMass, "kg"),
		formatMeasure("Technical Max Mass", base.TechnischeMaxMassaVoertuig, "kg"),
		formatMeasure("Max Towing Unbraked (Ongeremd)", base.MaximumMassaTrekkenOngeremd, "kg"),
		"Max Towing Braked (Geremd): " + withUnit(towingBraked, "kg"),
	}

	if base.MaximumMassaSamenstelling != "" {
		lines = append(lines, "Max Combination Mass: "+base.MaximumMassaSamenstelling+" kg")
	}

	if base.MassaAltAandr != "" {
		lines = append(lines, "Alternative Drive Mass: "+base.MassaAltAandr+" kg")
	}

	return strings.Join(lines, "\n")
}

func formatRegistration(base *VehicleBaseInfo) string {
	lines := []string{
		"First Registration: " + orUnknown(base.DatumEersteToelating),
		"First NL Registration: " + orUnknown(base.DatumEersteTenaamstellingInNederland),
	}

	if base.DatumTenaamstelling != "" {
		lines = append(lines, "Current Registration: "+base.DatumTenaamstelling)
	}

	typeApproval := orFirst(base.Typegoedkeuringsnummer, base.TypeGoedkeuringNummer)
	lines = append(lines,
		"Type Approval: "+typeApproval,
		"EU Type Approval Change Number: "+orUnknown(base.VolgnummerWijzigingEuTypegoedkeuring),
		"Registration Possible: "+orUnknown(base.TenaamstellenMogelijk),
		"Pending Inspection: "+orUnknown(base.WachtOpKeuren),
	)

	if base.RegistratieDatumGoedkeuringBpm != "" {
		lines = append(lines, "BPM Approval Date: "+base.RegistratieDatumGoedkeuringBpm)
	}

	return strings.Join(lines, "\n")
}

func formatInspection(base *VehicleBaseInfo) string {
	lines := []string{
		"APK Expiry: " + orUnknown(base.VervaldatumApk),
	}

	if base.VervaldatumTachograaf != "" {
		lines = append(lines, "Tachograph Expiry: "+base.VervaldatumTachograaf)
	}

	return strings.Join(lines, "\n")
}

func formatFinancial(base *VehicleBaseInfo) string {
	var lines []string

	if base.Catalogusprijs != "" {
		lines = append(lines, "Catalog Price: €"+base.Catalogusprijs)
	}

	if base.BrutoBpm != "" {
		lines = append(lines, "Gross BPM: €"+base.BrutoBpm)
	}

	return strings.Join(lines, "\n")
}

func formatIndicators(base *VehicleBaseInfo) string {
	lines := []string{
		"Fuel Efficiency Class: " + orFirst(base.Zuinigheidsclassificatie, base.Zuinigheidslabel),
		"Export Status: " + orFirst(base.ExportIndicator, base.Exportindicator),
		"Taxi: " + orUnknown(base.TaxiIndicator),
		"WAM Insured: " + orUnknown(base.WamVerzekerd),
		"Open Recall: " + orUnknown(base.OpenstaandeTerugroepactieIndicator),
		"Odometer Status: " + orUnknown(base.Tellerstandoordeel),
	}

	if base.CodeToelichtingTellerstandoordeel != "" {
		lines = append(lines, "Odometer Code: "+base.CodeToelichtingTellerstandoordeel)
	}

	lines = append(lines,
		"Aerodynamic Equipment: "+orUnknown(base.AerodynamicVoorziening),
		"Extended Cab: "+orUnknown(base.VerlengdeCabineIndicator),
	)

	return strings.Join(lines, "\n")
}

func formatFuel(fuels []VehicleFuelInfo) string {
	parts := make([]string, 0, len(fuels))

	for idx, fu := range fuels {
		lines := []string{
			fmt.Sprintf("Fuel Type %d: %s", idx+1, orUnknown(fu.BrandstofOmschrijving)),
			"Emission Code: " + orUnknown(fu.EmissiecodeOmschrijving),
			"Emission Level: " + orUnknown(fu.Uitlaatemissieniveau),
			"Environmental Class: " + orUnknown(fu.MilieuklasseEgGoedkeuringLicht),
			"CO2 Emission Class: " + orUnknown(fu.Co2Emissieklasse),
			"Max Power: " + orUnknown(fu.Nettomaximumvermogen) + " kW",
			"Sound Level (Driving): " + orUnknown(fu.GeluidsniveauRijdend) + " dB",
			"Sound Level (Idle): " + orUnknown(fu.GeluidsniveauStationair) + " dB",
			"Sound Test RPM: " + orUnknown(fu.ToerentalGeluidsniveau),
			"CO2 Combined (WLTP): " + orUnknown(fu.EmisCo2GecombineerdWltp) + " g/km",
			"Fuel Consumption Combined (WLTP): " + orUnknown(
				fu.BrandstofVerbruikGecombineerd,
			) + " l/100km",
			"Particle Emissions Type 1 (WLTP): " + orUnknown(fu.EmisDeeltjesType1Wltp) + " mg/km",
			"Soot Emission: " + orUnknown(fu.Roetuitstoot),
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n---\n")
}

func formatAxles(axles []VehicleAxesInfo) string {
	if len(axles) == 0 {
		return ""
	}

	lines := []string{
		"Total Axles: " + orUnknown(axles[0].AantalAssen),
	}

	for idx, ax := range axles {
		driven := drivenAxleLabel(ax.AangedrevenAs)
		axleNum := orFirst(ax.AsNummer, strconv.Itoa(idx+1))

		lines = append(lines,
			"Axle "+axleNum+":",
			"  Driven: "+driven,
			"  Position Code: "+orUnknown(ax.PlaatscodeAs),
			"  Track Width: "+orUnknown(ax.Spoorbreedte)+" cm",
			"  Technical Max Load: "+orUnknown(ax.TechnischToegestaneMaximumAslast)+" kg",
			"  Legal Max Load: "+orUnknown(ax.WettelijkToegestaneMaximumAslast)+" kg",
			"  Distance to Next Axle: "+orUnknown(ax.AfstandTotVolgendeAsVoertuig)+" cm",
		)
	}

	return strings.Join(lines, "\n")
}

func drivenAxleLabel(code string) string {
	switch code {
	case drivenAxleYes:
		return "Yes"
	case drivenAxleNo:
		return "No"
	default:
		return unknown
	}
}

func formatBody(bodies []VehicleBodyInfo) string {
	parts := make([]string, 0, len(bodies))

	for idx, bd := range bodies {
		lines := []string{
			fmt.Sprintf("Body Configuration %d:", idx+1),
			"Carrosserie Type: " + orUnknown(bd.Carrosserietype),
			"European Description: " + orUnknown(bd.TypeCarrosserieEuropeseOmschrijving),
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n---\n")
}

// formatRecalls renders the open-recall section.
func formatRecalls(recalls []VehicleRecall) string {
	parts := make([]string, 0, len(recalls))

	for idx, rc := range recalls {
		lines := []string{
			fmt.Sprintf("Recall %d:", idx+1),
			"RDW Reference: " + orUnknown(rc.ReferentiecodeRDW),
			"Status: " + orFirst(rc.Status, rc.CodeStatus),
		}

		if rc.PublicationDate != "" {
			lines = append(lines, "Published: "+formatRDWDate(rc.PublicationDate))
		}

		if rc.Description != "" {
			lines = append(lines, "Description: "+rc.Description)
		}

		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n---\n")
}

// formatAPKHistory renders APK inspection records.
func formatAPKHistory(records []VehicleAPKInspection) string {
	parts := make([]string, 0, len(records))

	for idx, rec := range records {
		lines := []string{
			fmt.Sprintf("Inspection %d:", idx+1),
			"Reported: " + formatRDWDate(rec.MeldDatumDoorKeuringsinstantie),
			"Type: " + orUnknown(rec.SoortMeldingKIOmschrijving),
			"Inspection Class: " + orUnknown(rec.SoortErkenningOmschrijving),
			"Valid Until: " + formatRDWDate(rec.VervaldatumKeuring),
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n---\n")
}

// formatDefects renders observed-defect records.
func formatDefects(defects []VehicleDefect) string {
	parts := make([]string, 0, len(defects))

	for idx, df := range defects {
		lines := []string{
			fmt.Sprintf("Defect %d:", idx+1),
			"Defect Code: " + orUnknown(df.GebrekIdentificatie),
			"Count: " + orUnknown(df.AantalGebrekenGeconstateerd),
			"Reported: " + formatRDWDate(df.MeldDatumDoorKeuringsinstantie),
			"Inspection Class: " + orUnknown(df.SoortErkenningOmschrijving),
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	return strings.Join(parts, "\n---\n")
}

// formatRDWDate formats an "YYYYMMDD" RDW date string as "YYYY-MM-DD". Inputs
// that don't match the expected layout are returned as-is (or "Unknown" when empty).
func formatRDWDate(value string) string {
	const expectedLen = 8
	if value == "" {
		return unknown
	}

	if len(value) != expectedLen {
		return value
	}

	for i := range expectedLen {
		if value[i] < '0' || value[i] > '9' {
			return value
		}
	}

	return value[:4] + "-" + value[4:6] + "-" + value[6:8]
}
