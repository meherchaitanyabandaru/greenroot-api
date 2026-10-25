package dispatches

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPublicTripCarriesNoPersonalData(t *testing.T) {
	s, n, f, id := "x", int64(7), 17.4, "trip-uuid"
	d := Dispatch{
		ID: 1, DispatchCode: "DSP-1", TripUUID: &id, Status: "DELIVERED", SellerNurseryName: &s,
		CustomerUserID: &n, CustomerNameSnapshot: &s, CustomerMobileSnapshot: &s, DriverUserID: &n,
		DriverName: &s, DriverMobile: &s, DestinationAddress: &s, DeliveryLatitude: &f, DeliveryLongitude: &f,
		Notes: &s, OutsideDriverMobile: &s, OutsideDriverName: &s, DeliveryOverrideReason: &s, DeliveryIssue: &s,
		OwnerUserIDSnapshot: &n, AssignedManagerUserID: &n, DispatchedBy: &n, DeliveryConfirmedByUserID: &n,
		TripEvents: []TripEvent{{EventType: "LOCATION", Latitude: &f, Longitude: &f, Remarks: &s, PhotoURL: &s, CreatedByUserID: n, CreatedAt: time.Now()}},
	}
	allowed := map[string]bool{
		"dispatch_code": true, "dispatch_status": true, "lifecycle": true, "seller_nursery_name": true,
		"vehicle_number": true, "dispatch_date": true, "trip_started_at": true, "delivery_date": true, "trip_events": true,
	}
	for _, status := range []string{"DELIVERED", "IN_TRANSIT"} {
		d.Status = status
		raw, _ := json.Marshal(publicTrip(withLifecycle(ActorContext{}, d)))
		var got map[string]json.RawMessage
		_ = json.Unmarshal(raw, &got)
		for k := range got {
			if !allowed[k] {
				t.Errorf("%s: public trip exposes %q", status, k)
			}
		}
		_, hasEvents := got["trip_events"]
		if hasEvents != (status == "IN_TRANSIT") {
			t.Errorf("%s: trip_events present=%v; positions only while on the way", status, hasEvents)
		}
		if hasEvents {
			var evs []map[string]any
			_ = json.Unmarshal(got["trip_events"], &evs)
			for k := range evs[0] {
				if k != "event_type" && k != "latitude" && k != "longitude" && k != "created_at" {
					t.Errorf("trip event exposes %q", k)
				}
			}
		}
	}
}
