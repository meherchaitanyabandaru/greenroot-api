// Package consent holds what each party confirms at each step of a trade
// (seller at loading, driver before the trip, buyer when accepting a
// quotation and at delivery) and records it with the exact wording, the
// version, who and when. Records are append-only. The wording states what
// each party is responsible for; it never waives anyone's legal rights.
package consent
