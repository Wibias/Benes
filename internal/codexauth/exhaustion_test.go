package codexauth

import (
	"testing"
	"time"
)

func TestQuotaExhaustedUsesPlanWindowAndBurst(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	future := float64(now.Add(time.Hour).Unix())
	weekly := 100.0
	monthly := 10.0
	short := 100.0
	if !QuotaExhaustedAt(&QuotaReading{WeeklyPercent: &weekly, WeeklyResetAt: &future}, "plus", now) {
		t.Fatal("weekly 100 should exhaust plus")
	}
	if QuotaExhaustedAt(&QuotaReading{WeeklyPercent: &weekly, WeeklyResetAt: &future, MonthlyPercent: &monthly, MonthlyResetAt: &future}, "go", now) {
		t.Fatal("go uses monthly window, not weekly")
	}
	if !QuotaExhaustedAt(&QuotaReading{ShortPercent: &short, ShortResetAt: &future, MonthlyPercent: &monthly, MonthlyResetAt: &future}, "go", now) {
		t.Fatal("burst window exhausts every plan")
	}
	if QuotaExhaustedAt(nil, "plus", now) {
		t.Fatal("missing quota is not exhausted")
	}
}
