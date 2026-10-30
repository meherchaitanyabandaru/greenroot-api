// Package risk flags unusual activity for admins: very large orders, prices
// far from the usual, repeated orders, cancellations, short loading,
// delivery disputes, payment mismatches and drivers with many disputes. A
// signal is a question for an admin, never a verdict: only an admin marks
// CONFIRMED_FRAUD, and only an admin holds an order (which pauses its
// dispatch). Thresholds live in risk_rules; every decision is a note.
package risk
