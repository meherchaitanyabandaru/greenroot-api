package subscriptions

import (
	"testing"
	"time"
)

func TestBusinessDayIsIndianCalendar(t *testing.T) {
	// 00:30 IST on 2 Jan is 19:00 UTC on 1 Jan.
	got := businessDay(time.Date(2026, 1, 1, 19, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("businessDay = %v, want %v", got, want)
	}
}
