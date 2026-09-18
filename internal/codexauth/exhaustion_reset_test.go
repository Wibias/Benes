package codexauth

import (
	"testing"
	"time"
)

func TestQuotaExhaustedAtHonorsResetEvidence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	full := 100.0
	partial := 40.0
	futureSeconds := float64(now.Add(time.Hour).Unix())
	pastSeconds := float64(now.Add(-time.Minute).Unix())
	futureMillis := float64(now.Add(time.Hour).UnixMilli())
	pastMillis := float64(now.Add(-time.Minute).UnixMilli())

	tests := []struct {
		name  string
		quota QuotaReading
		plan  string
		now   time.Time
		want  bool
	}{
		{
			name:  "full short with future reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &futureSeconds},
			plan:  "plus",
			now:   now,
			want:  true,
		},
		{
			name:  "full short after reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &pastSeconds},
			plan:  "plus",
			now:   now,
		},
		{
			name:  "full short missing reset",
			quota: QuotaReading{ShortPercent: &full},
			plan:  "plus",
			now:   now,
		},
		{
			name:  "full weekly with future reset",
			quota: QuotaReading{WeeklyPercent: &full, WeeklyResetAt: &futureSeconds},
			plan:  "plus",
			now:   now,
			want:  true,
		},
		{
			name:  "full weekly after reset",
			quota: QuotaReading{WeeklyPercent: &full, WeeklyResetAt: &pastSeconds},
			plan:  "plus",
			now:   now,
		},
		{
			name:  "go ignores weekly and uses live monthly",
			quota: QuotaReading{WeeklyPercent: &full, WeeklyResetAt: &futureSeconds, MonthlyPercent: &partial, MonthlyResetAt: &futureSeconds},
			plan:  "go",
			now:   now,
		},
		{
			name:  "go exhausts on live monthly",
			quota: QuotaReading{MonthlyPercent: &full, MonthlyResetAt: &futureSeconds},
			plan:  "free",
			now:   now,
			want:  true,
		},
		{
			name:  "stale short does not exhaust when weekly is live and partial",
			quota: QuotaReading{
				ShortPercent: &full, ShortResetAt: &pastSeconds,
				WeeklyPercent: &partial, WeeklyResetAt: &futureSeconds,
			},
			plan: "plus",
			now:  now,
		},
		{
			name:  "fresh short still exhausts when weekly is partial",
			quota: QuotaReading{
				ShortPercent: &full, ShortResetAt: &futureSeconds,
				WeeklyPercent: &partial, WeeklyResetAt: &futureSeconds,
			},
			plan: "plus",
			now:  now,
			want: true,
		},
		{
			name:  "seconds future reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &futureSeconds},
			plan:  "plus",
			now:   now,
			want:  true,
		},
		{
			name:  "milliseconds future reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &futureMillis},
			plan:  "plus",
			now:   now,
			want:  true,
		},
		{
			name:  "seconds past reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &pastSeconds},
			plan:  "plus",
			now:   now,
		},
		{
			name:  "milliseconds past reset",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &pastMillis},
			plan:  "plus",
			now:   now,
		},
		{
			name:  "seconds misread as milliseconds would look like 1970",
			quota: QuotaReading{ShortPercent: &full, ShortResetAt: &futureSeconds},
			plan:  "plus",
			now:   now,
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := QuotaExhaustedAt(&tc.quota, tc.plan, tc.now)
			if got != tc.want {
				t.Fatalf("exhausted=%v want=%v", got, tc.want)
			}
		})
	}
}
