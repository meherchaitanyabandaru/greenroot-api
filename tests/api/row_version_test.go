package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
)

type versionedOrder struct {
	Order struct {
		ID      int64  `json:"id"`
		Status  string `json:"order_status"`
		Version int64  `json:"version"`
	} `json:"order"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func orderRequest(t *testing.T, method, path string, body any, token, ifMatch string) (int, versionedOrder) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, baseURL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out versionedOrder
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestOrderRowVersion checks optimistic concurrency end to end: responses
// carry `version`, a stale If-Match is refused with 409 version_conflict and
// changes nothing, a malformed one is 400, a current one succeeds, and two
// racing writers cannot both win.
func TestOrderRowVersion(t *testing.T) {
	ownerToken := login(t, ownerPhone)
	nurseryID := getOwnerNurseryID(t, ownerToken)
	orderID, _ := createLifecycleOrder(t, ownerToken, nurseryID, buyerPhone)
	path := fmt.Sprintf("/api/v1/orders/%d", orderID)

	code, o := orderRequest(t, http.MethodGet, path, nil, ownerToken, "")
	if code != http.StatusOK || o.Order.Version < 1 {
		t.Fatalf("GET order: %d version=%d", code, o.Order.Version)
	}
	v := o.Order.Version
	confirm := map[string]any{"order_status": "CONFIRMED"}

	code, o = orderRequest(t, http.MethodPut, path+"/status", confirm, ownerToken, strconv.FormatInt(v+100, 10))
	if code != http.StatusConflict || o.Error.Code != "version_conflict" {
		t.Fatalf("stale If-Match: %d %q, want 409 version_conflict", code, o.Error.Code)
	}
	if _, cur := orderRequest(t, http.MethodGet, path, nil, ownerToken, ""); cur.Order.Status != "PENDING" || cur.Order.Version != v {
		t.Fatalf("stale write changed the order: %+v", cur.Order)
	}

	code, o = orderRequest(t, http.MethodPut, path+"/status", confirm, ownerToken, "not-a-version")
	if code != http.StatusBadRequest || o.Error.Code != "invalid_if_match" {
		t.Fatalf("malformed If-Match: %d %q, want 400 invalid_if_match", code, o.Error.Code)
	}

	code, o = orderRequest(t, http.MethodPut, path+"/status", confirm, ownerToken, fmt.Sprintf(`"%d"`, v))
	if code != http.StatusOK || o.Order.Status != "CONFIRMED" || o.Order.Version <= v {
		t.Fatalf("current If-Match: %d status=%q version=%d (was %d)", code, o.Order.Status, o.Order.Version, v)
	}

	// Two writers holding the same version: exactly one may win.
	held := strconv.FormatInt(o.Order.Version, 10)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], _ = orderRequest(t, http.MethodPost, path+"/cancel", map[string]any{"reason": "race"}, ownerToken, held)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
		default:
			t.Fatalf("racing cancel returned %d", c)
		}
	}
	if wins != 1 {
		t.Fatalf("racing cancels: %v, want exactly one 200", codes)
	}
}
