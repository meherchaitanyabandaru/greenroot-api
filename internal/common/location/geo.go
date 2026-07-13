// Package location provides reusable PostGIS SQL expression builders.
// Import this package wherever a repository needs ST_DWithin or ST_Distance.
// All functions return SQL fragments — no external dependencies required.
package location

import "fmt"

// MakePoint returns the PostGIS geography literal for a lon/lat arg pair.
//
//	MakePoint(2, 1) → ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography
//
// Note: PostGIS MakePoint(lon, lat) — longitude comes first.
func MakePoint(lonArgIdx, latArgIdx int) string {
	return fmt.Sprintf(
		"ST_SetSRID(ST_MakePoint($%d::double precision, $%d::double precision), 4326)::geography",
		lonArgIdx, latArgIdx,
	)
}

// DWithin returns a PostGIS ST_DWithin expression that filters rows within
// radiusM metres of the given lon/lat arguments.
//
//	DWithin("na.location", 2, 1, 50000) →
//	  ST_DWithin(na.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, 50000)
func DWithin(colExpr string, lonArgIdx, latArgIdx int, radiusM float64) string {
	return fmt.Sprintf("ST_DWithin(%s, %s, %g)", colExpr, MakePoint(lonArgIdx, latArgIdx), radiusM)
}

// DistanceKM returns a PostGIS ST_Distance expression (in kilometres).
//
//	DistanceKM("na.location", 2, 1) →
//	  ST_Distance(na.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography) / 1000.0
func DistanceKM(colExpr string, lonArgIdx, latArgIdx int) string {
	return fmt.Sprintf("ST_Distance(%s, %s) / 1000.0", colExpr, MakePoint(lonArgIdx, latArgIdx))
}
