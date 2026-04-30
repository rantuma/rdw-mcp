package rdw

import (
	"context"
	"net/http"
)

// ExportMakeRDWRequestBase exposes the unexported generic makeRDWRequest
// instantiated for VehicleBaseInfo, for use in external package tests.
func ExportMakeRDWRequestBase(
	ctx context.Context,
	client *http.Client,
	endpoint, kenteken string,
) ([]VehicleBaseInfo, error) {
	return makeRDWRequest[VehicleBaseInfo](ctx, client, endpoint, kenteken)
}

// ExportBuildResult exposes buildResult for external package tests.
func ExportBuildResult(
	base VehicleBaseInfo,
	fuelRecords []VehicleFuelInfo, fuelErr error,
	axleRecords []VehicleAxesInfo, axleErr error,
	bodyRecords []VehicleBodyInfo, bodyErr error,
) *AllVehicleData {
	return buildResult(base, fuelRecords, fuelErr, axleRecords, axleErr, bodyRecords, bodyErr)
}
