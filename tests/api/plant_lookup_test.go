package apitest

import (
	"database/sql"
	"net/http"
	"os"
	"sync"
	"testing"
)

// TestPlantLookupAndResolve: pickers find botanical names that are not in the
// catalog, picking one adds it once (synonyms map to the accepted species,
// concurrent picks share one plant), and an admin-removed species stays removed.
// Uses its own wfo-test-* names so it does not depend on the 1.66 M WFO load.
func TestPlantLookupAndResolve(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // registered first, so it runs after cleanup
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	cleanup := func() {
		exec(`DELETE FROM public.plants WHERE wfo_id LIKE 'wfo-test-%' OR scientific_name LIKE 'Testflora %'`)
		exec(`DELETE FROM public.botanical_names WHERE wfo_id LIKE 'wfo-test-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	exec(`INSERT INTO public.botanical_names (wfo_id, scientific_name, taxon_rank, family, genus, status, accepted_wfo_id) VALUES
		('wfo-test-1', 'Testflora qzxjunii', 'species', 'Testaceae', 'Testflora', 'ACCEPTED', NULL),
		('wfo-test-2', 'Oldflora qzxjunii',  'species', 'Testaceae', 'Oldflora',  'SYNONYM',  'wfo-test-1'),
		('wfo-test-3', 'Testflora removedii', 'species', 'Testaceae', 'Testflora', 'ACCEPTED', NULL),
		('wfo-test-4', 'Testflora', 'genus', 'Testaceae', 'Testflora', 'ACCEPTED', NULL),
		('wfo-test-5', 'Testflora linkii', 'species', 'Testaceae', 'Testflora', 'ACCEPTED', NULL)`)

	owner := login(t, "9100000000")
	admin := login(t, "9000000000")

	type plant struct {
		ID             int64  `json:"id"`
		ScientificName string `json:"scientific_name"`
		WfoID          string `json:"wfo_id"`
	}
	type lookup struct {
		Results []struct {
			Plant *plant `json:"plant"`
			Name  *struct {
				WfoID         string `json:"wfo_id"`
				AcceptedWfoID string `json:"accepted_wfo_id"`
			} `json:"name"`
		} `json:"results"`
	}
	search := func(q string) lookup {
		t.Helper()
		resp := get(t, "/api/v1/plants/lookup?limit=50&q="+q, owner)
		if resp.StatusCode != http.StatusOK {
			assertStatus(t, resp, http.StatusOK)
			t.FailNow()
		}
		var l lookup
		decode(t, resp, &l)
		return l
	}
	resolve := func(wfoID string) (plant, int) {
		t.Helper()
		resp := post(t, "/api/v1/plants/resolve", map[string]string{"wfo_id": wfoID}, owner)
		var body struct {
			Plant plant `json:"plant"`
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return plant{}, resp.StatusCode
		}
		decode(t, resp, &body)
		return body.Plant, resp.StatusCode
	}

	// One row per species (accepted name and its synonym share one), no genus rows.
	l := search("qzxjunii")
	if len(l.Results) != 1 || l.Results[0].Name == nil {
		t.Fatalf("want one botanical row for the qzxjunii species, got %+v", l.Results)
	}
	for _, r := range search("testflora").Results {
		if r.Name != nil && r.Name.WfoID == "wfo-test-4" {
			t.Fatal("a genus must not be pickable")
		}
	}

	// Picking the synonym five times at once adds one plant: the accepted species.
	var wg sync.WaitGroup
	got := make([]plant, 5)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], _ = resolve("wfo-test-2")
		}(i)
	}
	wg.Wait()
	for _, p := range got {
		if p.ID == 0 || p.ID != got[0].ID || p.ScientificName != "Testflora qzxjunii" || p.WfoID != "wfo-test-1" {
			t.Fatalf("concurrent resolves must return one accepted plant, got %+v", got)
		}
	}
	if accepted, _ := resolve("wfo-test-1"); accepted.ID != got[0].ID {
		t.Fatalf("accepted name must resolve to the same plant %d, got %d", got[0].ID, accepted.ID)
	}

	// Now the species is a catalog result, not a botanical one.
	l = search("qzxjunii")
	if len(l.Results) != 1 || l.Results[0].Plant == nil || l.Results[0].Plant.ID != got[0].ID {
		t.Fatalf("resolved species must come back as the catalog plant, got %+v", l.Results)
	}

	// A plant typed in by hand before the names existed is linked, not duplicated.
	resp := post(t, "/api/v1/plants", map[string]any{"scientific_name": "Testflora linkii"}, admin)
	var created struct {
		Plant plant `json:"plant"`
	}
	decode(t, resp, &created)
	if linked, _ := resolve("wfo-test-5"); linked.ID != created.Plant.ID || linked.WfoID != "wfo-test-5" {
		t.Fatalf("hand-entered plant %d must be linked, got %+v", created.Plant.ID, linked)
	}

	// An admin-removed species is not silently re-added.
	removed, _ := resolve("wfo-test-3")
	assertStatus(t, deleteReq(t, url("/api/v1/plants/%d", removed.ID), admin), http.StatusOK)
	if _, status := resolve("wfo-test-3"); status != http.StatusConflict {
		t.Fatalf("resolve of a removed species: got %d, want 409", status)
	}

	// Admin → Plants / Categories browse the same names with their catalog link.
	type browseResp struct {
		Names []struct {
			WfoID            string `json:"wfo_id"`
			CatalogPlantCode string `json:"catalog_plant_code"`
			SynonymCount     int64  `json:"synonym_count"`
			MatchedName      string `json:"matched_name"`
		} `json:"names"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
	}
	// Default: one row per plant; the old name is counted, not listed.
	var browse browseResp
	decode(t, get(t, "/api/v1/botanical-names?search=qzxjunii", admin), &browse)
	if browse.Pagination.Total != 1 || browse.Names[0].WfoID != "wfo-test-1" || browse.Names[0].SynonymCount != 1 {
		t.Fatalf("browse: want one plant row with 1 old name, got %+v", browse)
	}
	// Searching the old name's genus finds the plant and says which old name matched.
	decode(t, get(t, "/api/v1/botanical-names?search=oldflora", admin), &browse)
	if browse.Pagination.Total != 1 || browse.Names[0].WfoID != "wfo-test-1" || browse.Names[0].MatchedName != "Oldflora qzxjunii" {
		t.Fatalf("old-name search: %+v", browse)
	}
	resp = get(t, "/api/v1/botanical-names?search=qzxjunii&show_synonyms=true", admin)
	decode(t, resp, &browse)
	if browse.Pagination.Total != 2 || len(browse.Names) != 2 || browse.Names[0].WfoID != "wfo-test-1" {
		t.Fatalf("browse with old names: want accepted name first of 2, got %+v", browse)
	}
	for _, n := range browse.Names {
		if n.CatalogPlantCode == "" {
			t.Fatalf("both names of the resolved species must show its catalog plant: %+v", browse.Names)
		}
	}
	resp = get(t, "/api/v1/botanical-names/families", admin)
	var fams struct {
		Families []struct {
			Family    string `json:"family"`
			Names     int64  `json:"names"`
			InCatalog int64  `json:"in_catalog"`
		} `json:"families"`
	}
	decode(t, resp, &fams)
	found := false
	for _, f := range fams.Families {
		if f.Family == "Testaceae" {
			found = f.Names == 5 && f.InCatalog >= 1
		}
	}
	if !found {
		t.Fatal("families must count the 5 Testaceae names with at least one in the catalog")
	}
	resp = get(t, "/api/v1/botanical-names/wfo-test-2", admin)
	var detail struct {
		Accepted struct {
			WfoID string `json:"wfo_id"`
		} `json:"accepted"`
		SynonymCount int64 `json:"synonym_count"`
	}
	decode(t, resp, &detail)
	if detail.Accepted.WfoID != "wfo-test-1" || detail.SynonymCount != 1 {
		t.Fatalf("synonym detail: %+v", detail)
	}
	assertStatus(t, get(t, "/api/v1/botanical-names", owner), http.StatusForbidden)

	if _, status := resolve("wfo-test-nope"); status != http.StatusNotFound {
		t.Fatalf("unknown name: got %d, want 404", status)
	}
	assertStatus(t, get(t, "/api/v1/plants/lookup?q=qzxjunii", ""), http.StatusUnauthorized)
}
