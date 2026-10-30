package quotations

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ─── mock repository: requests ──────────────────────────────────────────────

func (m *mockRepo) RequestTarget(_ context.Context, nurseryID int64) (requestTarget, error) {
	owner, ok := m.nurseryOwners[nurseryID]
	if !ok {
		return requestTarget{}, ErrNotFound
	}
	return requestTarget{OwnerID: owner, OwnerName: "Owner", Approved: m.activeNurseries[nurseryID]}, nil
}

func (m *mockRepo) CountRecentRequests(_ context.Context, userID int64) (int, error) {
	n := 0
	for _, q := range m.quotations {
		if q.RequestedByUserID != nil && *q.RequestedByUserID == userID {
			n++
		}
	}
	return n, nil
}

func (m *mockRepo) CreateRequest(_ context.Context, customerID int64, in CreateRequestInput, t requestTarget, fp string) (*Quotation, error) {
	for _, q := range m.quotations {
		if q.Status == "CUSTOMER_REQUESTED" && q.RequestedByUserID != nil && *q.RequestedByUserID == customerID && q.Notes != nil && *q.Notes == fp {
			return nil, ErrDuplicateRequest
		}
	}
	id := int64(len(m.quotations) + 1)
	exp := time.Now().Add(requestWindow)
	q := &Quotation{ID: id, QuotationCode: "QUO-R", QuotationType: "CUSTOMER", Status: "CUSTOMER_REQUESTED",
		NurseryID: &in.NurseryID, CreatedByUserID: t.OwnerID, CustomerUserID: &customerID, RequestedByUserID: &customerID,
		IsRequest: true, RequestExpiresAt: &exp, RequestNote: in.Note, Notes: &fp}
	for i, it := range in.Items {
		item := QuotationItem{ID: id*100 + int64(i), QuotationID: id, Quantity: it.Quantity, CustomPlantName: it.CustomPlantName, IsCustom: it.PlantID == 0}
		if it.PlantID > 0 {
			pid := it.PlantID
			item.PlantID = &pid
		}
		q.Items = append(q.Items, item)
	}
	m.quotations[id] = q
	cp := *q
	return &cp, nil
}

func (m *mockRepo) PriceRequest(_ context.Context, id int64, in PriceRequestInput) (*Quotation, error) {
	q := m.quotations[id]
	for _, l := range in.Items {
		for i := range q.Items {
			if q.Items[i].ID == l.ItemID {
				q.Items[i].UnitPrice, q.Items[i].IsPriced = l.UnitPrice, true
				if l.Quantity != nil {
					q.Items[i].Quantity = *l.Quantity
				}
			}
		}
	}
	cp := *q
	return &cp, nil
}

func (m *mockRepo) CloseRequest(_ context.Context, id int64, status string, reason *string) (*Quotation, error) {
	q := m.quotations[id]
	if q.Status != "CUSTOMER_REQUESTED" {
		return nil, ErrInvalidTransition
	}
	q.Status, q.DeclineReason = status, reason
	cp := *q
	return &cp, nil
}

// ─── tests ───────────────────────────────────────────────────────────────────

func requestIn() CreateRequestInput {
	return CreateRequestInput{NurseryID: 1, Note: sp(" need by Friday "), Items: []QuotationItemRequest{
		{PlantID: 7, Quantity: 50},
		{CustomPlantName: sp("Nela vemu"), Variety: sp("dwarf"), Quantity: 20},
		{CustomPlantName: sp("Banginapalli mango"), SizeDescription: sp("3 ft"), Quantity: 10},
	}}
}

func TestCreateRequest_WhoMayAsk(t *testing.T) {
	cases := []struct {
		name  string
		actor ActorContext
		want  error
	}{
		{"a customer", buyerActor(50), nil},
		{"the nursery's own owner", ownerActor(10), ErrOwnNursery},
		{"its manager", managerActor(11), ErrOwnNursery},
		{"an admin", adminActor(1), ErrForbidden},
		{"a driver", ActorContext{UserID: 60, Roles: []string{"DRIVER"}}, ErrForbidden},
	}
	for _, c := range cases {
		repo := newMockRepo()
		repo.addNursery(1, 10, 11)
		_, err := svc(repo).CreateRequest(context.Background(), c.actor, requestIn())
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestCreateRequest_LinesAndRules(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(1, 10)
	q, err := svc(repo).CreateRequest(context.Background(), buyerActor(50), requestIn())
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != "CUSTOMER_REQUESTED" || len(q.Items) != 3 || !q.Items[1].IsCustom || *q.Items[1].CustomPlantName != "Nela vemu" {
		t.Fatalf("request with catalog and custom lines: %+v", q)
	}
	if q.Capabilities == nil || !q.Capabilities.CanCancelRequest || q.Capabilities.CanPrice {
		t.Errorf("the customer may cancel, not price: %+v", q.Capabilities)
	}
	if _, err := svc(repo).CreateRequest(context.Background(), buyerActor(50), requestIn()); !errors.Is(err, ErrDuplicateRequest) {
		t.Errorf("the same request twice: want ErrDuplicateRequest, got %v", err)
	}
	bad := requestIn()
	bad.Items[0].UnitPrice = 0
	bad.Items = append(bad.Items, QuotationItemRequest{Quantity: 3}) // neither plant nor name
	if _, err := svc(repo).CreateRequest(context.Background(), buyerActor(51), bad); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("a line without a plant: want ErrInvalidInput, got %v", err)
	}
	repo.activeNurseries[1] = false
	if _, err := svc(repo).CreateRequest(context.Background(), buyerActor(52), requestIn()); !errors.Is(err, ErrNurseryUnavailable) {
		t.Errorf("a nursery not approved: want ErrNurseryUnavailable, got %v", err)
	}
}

func TestRequestFingerprint(t *testing.T) {
	a, b := requestIn(), requestIn()
	b.Items[0], b.Items[2] = b.Items[2], b.Items[0]
	b.Items[1].CustomPlantName = sp("nela  VEMU")
	if requestFingerprint(a) != requestFingerprint(b) {
		t.Error("line order and spelling case do not make a new request")
	}
	b.Items[0].Quantity = 11
	if requestFingerprint(a) == requestFingerprint(b) {
		t.Error("a different quantity is a different request")
	}
}

func TestPriceAndSendRequest(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(1, 10, 11)
	s := svc(repo)
	q, err := s.CreateRequest(context.Background(), buyerActor(50), requestIn())
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerActor(10)
	if _, err := s.SendToCustomer(context.Background(), owner, q.ID); !errors.Is(err, ErrRequestNotPriced) {
		t.Errorf("send before pricing: want ErrRequestNotPriced, got %v", err)
	}
	if _, err := s.PriceRequest(context.Background(), buyerActor(50), q.ID, PriceRequestInput{Items: []PriceLine{{ItemID: q.Items[0].ID, UnitPrice: 5}}}); !errors.Is(err, ErrForbidden) {
		t.Errorf("the customer cannot price: want ErrForbidden, got %v", err)
	}
	if _, err := s.PriceRequest(context.Background(), owner, q.ID, PriceRequestInput{Items: []PriceLine{{ItemID: 999, UnitPrice: 5}}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("a line of another quotation: want ErrInvalidInput, got %v", err)
	}
	less := 15.0
	lines := []PriceLine{{ItemID: q.Items[0].ID, UnitPrice: 40}, {ItemID: q.Items[1].ID, UnitPrice: 25, Quantity: &less}, {ItemID: q.Items[2].ID, UnitPrice: 300}}
	priced, err := s.PriceRequest(context.Background(), owner, q.ID, PriceRequestInput{Items: lines})
	if err != nil {
		t.Fatal(err)
	}
	if !priced.Items[1].IsCustom || priced.Items[1].Quantity != 15 || !priced.Capabilities.CanSend {
		t.Errorf("custom line stays custom, quantity supplied, ready to send: %+v", priced)
	}
	if _, err := s.SendToCustomer(context.Background(), owner, q.ID); err != nil {
		t.Errorf("send a priced request: %v", err)
	}
}

func TestRequestExpiryAndClosing(t *testing.T) {
	repo := newMockRepo()
	repo.addNursery(1, 10)
	s := svc(repo)
	q, _ := s.CreateRequest(context.Background(), buyerActor(50), requestIn())
	past := time.Now().Add(-time.Hour)
	repo.quotations[q.ID].RequestExpiresAt = &past
	if _, err := s.PriceRequest(context.Background(), ownerActor(10), q.ID, PriceRequestInput{Items: []PriceLine{{ItemID: q.Items[0].ID, UnitPrice: 1}}}); !errors.Is(err, ErrRequestExpired) {
		t.Errorf("an expired request: want ErrRequestExpired, got %v", err)
	}
	if _, err := s.CancelRequest(context.Background(), buyerActor(99), q.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("someone else cancels: want ErrForbidden, got %v", err)
	}
	if _, err := s.DeclineRequest(context.Background(), ownerActor(10), q.ID, "no"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("decline needs a reason: want ErrInvalidInput, got %v", err)
	}
	got, err := s.DeclineRequest(context.Background(), ownerActor(10), q.ID, "out of stock this season")
	if err != nil || got.Status != "REQUEST_DECLINED" {
		t.Fatalf("decline: %+v %v", got, err)
	}
	if _, err := s.CancelRequest(context.Background(), buyerActor(50), q.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("cancel a declined request: want ErrInvalidTransition, got %v", err)
	}
}

func svc(repo *mockRepo) *Service { return NewService(repo, nil, nil) }
