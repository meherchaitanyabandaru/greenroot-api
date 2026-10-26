// Package reports serves date-range reports (orders, quotations, deliveries)
// as JSON summaries with paged rows, or as a streamed CSV download. Each
// caller sees only their own slice: an owner their nursery, a manager the
// work assigned to or created by them, a driver their trips, a customer
// their purchases, an admin everything.
package reports
