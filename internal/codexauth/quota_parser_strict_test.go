package codexauth

import "testing"

func TestParseWHAMUsageRejectsTrailingNumericStringJunk(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":"150x"}}}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.Quota != nil {
		t.Fatalf("quota=%#v", result.Quota)
	}
}

func TestParseWHAMUsageDoesNotTreatStringDurationAsExplicitBurstWindow(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{
		"rate_limit":{"primary_window":{"used_percent":90,"limit_window_seconds":"18000"}}
	}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.Quota == nil || result.Quota.WeeklyPercent == nil || *result.Quota.WeeklyPercent != 90 {
		t.Fatalf("quota=%#v", result.Quota)
	}
	if result.Quota.ShortPercent != nil {
		t.Fatalf("string duration became explicit burst evidence: %#v", result.Quota)
	}
}
