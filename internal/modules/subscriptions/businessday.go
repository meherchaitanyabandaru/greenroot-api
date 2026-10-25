package subscriptions

import "time"

// businessZone is the calendar subscriptions run on. Truncating to UTC
// midnight put payments made 00:00-05:30 IST on the previous day.
var businessZone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return loc
	}
	return time.FixedZone("IST", 5*3600+1800) // no tzdata in the image
}()

// businessDay is t's calendar date in India, as midnight UTC (how DATE
// columns are written and read back).
func businessDay(t time.Time) time.Time {
	y, m, d := t.In(businessZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
