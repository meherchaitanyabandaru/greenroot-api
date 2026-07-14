package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ─── mock repository ─────────────────────────────────────────────────────────

type mockRepo struct {
	points     []TrackingPoint
	dispatches map[int64]DispatchAccess
	nextID     int64
}

func newMock() *mockRepo { return &mockRepo{dispatches: map[int64]DispatchAccess{}, nextID: 100} }

func (m *mockRepo) Create(_ context.Context, in CreateRequest) (*TrackingPoint, error) {
	m.nextID++
	p := &TrackingPoint{
		ID:         m.nextID,
		VehicleID:  in.VehicleID,
		DriverID:   in.DriverID,
		DispatchID: in.DispatchID,
		Latitude:   in.Latitude,
		Longitude:  in.Longitude,
		TrackedAt:  time.Now(),
		Notes:      in.Notes,
	}
	m.points = append(m.points, *p)
	return p, nil
}

func (m *mockRepo) DispatchAccess(_ context.Context, dispatchID int64) (*DispatchAccess, error) {
	access, ok := m.dispatches[dispatchID]
	if !ok {
		return nil, ErrInvalidInput
	}
	return &access, nil
}

func (m *mockRepo) ListBy(_ context.Context, col string, id int64) ([]TrackingPoint, error) {
	var result []TrackingPoint
	for _, p := range m.points {
		switch col {
		case "vehicle_id":
			if p.VehicleID != nil && *p.VehicleID == id {
				result = append(result, p)
			}
		case "driver_id":
			if p.DriverID != nil && *p.DriverID == id {
				result = append(result, p)
			}
		case "dispatch_id":
			if p.DispatchID != nil && *p.DispatchID == id {
				result = append(result, p)
			}
		}
	}
	return result, nil
}

func (m *mockRepo) LatestBy(_ context.Context, col string, id int64) (*TrackingPoint, error) {
	pts, _ := m.ListBy(context.Background(), col, id)
	if len(pts) == 0 {
		return nil, nil
	}
	p := pts[len(pts)-1]
	return &p, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func ptr[T any](v T) *T { return &v }

func adminActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"ADMIN"}} }
func driverActor(id int64) ActorContext { return ActorContext{UserID: id, Roles: []string{"DRIVER"}} }
func buyerActor(id int64) ActorContext  { return ActorContext{UserID: id, Roles: []string{"BUYER"}} }
func ownerActor(id int64) ActorContext {
	return ActorContext{UserID: id, Roles: []string{"NURSERY_OWNER"}}
}

func svc(repo *mockRepo) *Service { return NewService(repo) }

func (m *mockRepo) seedDispatch(id int64, status string, driverUserID *int64) {
	m.dispatches[id] = DispatchAccess{Status: status, DriverUserID: driverUserID}
}

// ─── Create ──────────────────────────────────────────────────────────────────

func TestCreate_DriverWithVehicle(t *testing.T) {
	p, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
		VehicleID: ptr(int64(5)),
		Latitude:  12.97,
		Longitude: 77.59,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.VehicleID == nil || *p.VehicleID != 5 {
		t.Errorf("want VehicleID=5, got %v", p.VehicleID)
	}
}

func TestCreate_AdminWithDispatch(t *testing.T) {
	repo := newMock()
	repo.seedDispatch(10, "IN_TRANSIT", nil)
	_, err := svc(repo).Create(context.Background(), adminActor(1), CreateRequest{
		DispatchID: ptr(int64(10)),
		Latitude:   12.97,
		Longitude:  77.59,
	})
	if err != nil {
		t.Fatalf("admin should create tracking point: %v", err)
	}
}

func TestCreate_DriverDispatchMustBeInTransit(t *testing.T) {
	repo := newMock()
	repo.seedDispatch(10, "ACCEPTED", ptr(int64(7)))

	_, err := svc(repo).Create(context.Background(), driverActor(7), CreateRequest{
		DispatchID: ptr(int64(10)),
		Latitude:   12.97,
		Longitude:  77.59,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("tracking before IN_TRANSIT: want ErrInvalidInput, got %v", err)
	}
}

func TestCreate_DriverDispatchMustBeAssigned(t *testing.T) {
	repo := newMock()
	repo.seedDispatch(10, "IN_TRANSIT", ptr(int64(8)))

	_, err := svc(repo).Create(context.Background(), driverActor(7), CreateRequest{
		DispatchID: ptr(int64(10)),
		Latitude:   12.97,
		Longitude:  77.59,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("tracking by unassigned driver: want ErrForbidden, got %v", err)
	}
}

func TestCreate_DriverWithDriverID(t *testing.T) {
	p, err := svc(newMock()).Create(context.Background(), driverActor(7), CreateRequest{
		DriverID:  ptr(int64(7)),
		Latitude:  28.6,
		Longitude: 77.2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DriverID == nil || *p.DriverID != 7 {
		t.Errorf("want DriverID=7, got %v", p.DriverID)
	}
}

func TestCreate_BuyerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), buyerActor(1), CreateRequest{
		VehicleID: ptr(int64(1)),
		Latitude:  12.97,
		Longitude: 77.59,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for buyer, got %v", err)
	}
}

func TestCreate_OwnerForbidden(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), ownerActor(1), CreateRequest{
		VehicleID: ptr(int64(1)),
		Latitude:  12.97,
		Longitude: 77.59,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("want ErrForbidden for nursery owner, got %v", err)
	}
}

func TestCreate_NoTargetInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
		Latitude:  12.97,
		Longitude: 77.59,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for no target, got %v", err)
	}
}

func TestCreate_LatitudeTooHighInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
		VehicleID: ptr(int64(1)),
		Latitude:  91.0,
		Longitude: 77.59,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for lat=91, got %v", err)
	}
}

func TestCreate_LatitudeTooLowInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
		VehicleID: ptr(int64(1)),
		Latitude:  -91.0,
		Longitude: 77.59,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for lat=-91, got %v", err)
	}
}

func TestCreate_LongitudeTooHighInvalid(t *testing.T) {
	_, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
		VehicleID: ptr(int64(1)),
		Latitude:  12.97,
		Longitude: 181.0,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for lng=181, got %v", err)
	}
}

func TestCreate_BoundaryCoordinatesValid(t *testing.T) {
	cases := []struct{ lat, lng float64 }{
		{-90, 0}, {90, 0}, {0, -180}, {0, 180},
	}
	for _, c := range cases {
		_, err := svc(newMock()).Create(context.Background(), driverActor(1), CreateRequest{
			VehicleID: ptr(int64(1)),
			Latitude:  c.lat,
			Longitude: c.lng,
		})
		if err != nil {
			t.Errorf("boundary (%v, %v) should be valid, got %v", c.lat, c.lng, err)
		}
	}
}

// ─── List ────────────────────────────────────────────────────────────────────

func TestList_ByVehicle(t *testing.T) {
	repo := newMock()
	v := int64(5)
	repo.points = []TrackingPoint{
		{ID: 1, VehicleID: &v, Latitude: 12.97, Longitude: 77.59},
		{ID: 2, VehicleID: &v, Latitude: 12.98, Longitude: 77.60},
	}

	pts, err := svc(repo).List(context.Background(), adminActor(1), "vehicle_id", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pts) != 2 {
		t.Errorf("want 2 points, got %d", len(pts))
	}
}

func TestList_ByDriver(t *testing.T) {
	repo := newMock()
	d := int64(7)
	repo.points = []TrackingPoint{
		{ID: 1, DriverID: &d, Latitude: 12.97, Longitude: 77.59},
	}

	pts, err := svc(repo).List(context.Background(), driverActor(7), "driver_id", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pts) != 1 {
		t.Errorf("want 1 point, got %d", len(pts))
	}
}

func TestList_EmptyResult(t *testing.T) {
	pts, err := svc(newMock()).List(context.Background(), adminActor(1), "vehicle_id", 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pts == nil {
		pts = []TrackingPoint{}
	}
	if len(pts) != 0 {
		t.Errorf("want 0 points, got %d", len(pts))
	}
}

func TestList_ZeroIDInvalid(t *testing.T) {
	_, err := svc(newMock()).List(context.Background(), adminActor(1), "vehicle_id", 0)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for id=0, got %v", err)
	}
}

func TestList_NegativeIDInvalid(t *testing.T) {
	_, err := svc(newMock()).List(context.Background(), adminActor(1), "vehicle_id", -1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for id=-1, got %v", err)
	}
}

// ─── Latest ──────────────────────────────────────────────────────────────────

func TestLatest_ReturnsPoint(t *testing.T) {
	repo := newMock()
	v := int64(5)
	repo.points = []TrackingPoint{
		{ID: 1, VehicleID: &v, Latitude: 12.97, Longitude: 77.59},
	}

	p, err := svc(repo).Latest(context.Background(), adminActor(1), "vehicle_id", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("want a point, got nil")
	}
	if p.Latitude != 12.97 {
		t.Errorf("want lat 12.97, got %v", p.Latitude)
	}
}

func TestLatest_ReturnsNilWhenNone(t *testing.T) {
	p, err := svc(newMock()).Latest(context.Background(), adminActor(1), "vehicle_id", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Errorf("want nil for empty result, got %v", p)
	}
}

func TestLatest_ZeroIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Latest(context.Background(), adminActor(1), "vehicle_id", 0)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for id=0, got %v", err)
	}
}

func TestLatest_NegativeIDInvalid(t *testing.T) {
	_, err := svc(newMock()).Latest(context.Background(), adminActor(1), "driver_id", -5)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("want ErrInvalidInput for id=-5, got %v", err)
	}
}
