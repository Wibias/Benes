package quota

import "testing"

func TestStoreSnapshotReturnsDeepCopyWithoutProbing(t *testing.T) {
	fiveHour := 100.0
	weekly := 42.0
	store := &Store{value: Response{
		GeneratedAt: 1234,
		Reports: []Report{{
			Provider: "example",
			Quota: Quota{
				FiveHourPercent: &fiveHour,
				WeeklyPercent: &weekly,
				CustomWindows: []Window{{Label: "Burst", Percent: 88}},
				CreditsUsd: &CreditsUsd{Used: 1, Limit: 2, Remaining: 1, Percent: 50},
			},
			Entitlement: &Entitlement{PlanRenewsAt: 9999},
			UpdatedAt: 1200,
		}},
	}}

	got := store.Snapshot()
	if got.GeneratedAt != 1234 || len(got.Reports) != 1 || got.Reports[0].Provider != "example" {
		t.Fatalf("snapshot=%#v", got)
	}
	*got.Reports[0].Quota.FiveHourPercent = 1
	got.Reports[0].Quota.CustomWindows[0].Percent = 1
	got.Reports[0].Quota.CreditsUsd.Percent = 1
	got.Reports[0].Entitlement.PlanRenewsAt = 1

	again := store.Snapshot()
	if again.Reports[0].Quota.FiveHourPercent == nil || *again.Reports[0].Quota.FiveHourPercent != 100 {
		t.Fatalf("five-hour quota aliased: %#v", again.Reports[0].Quota)
	}
	if again.Reports[0].Quota.CustomWindows[0].Percent != 88 {
		t.Fatalf("custom window aliased: %#v", again.Reports[0].Quota.CustomWindows)
	}
	if again.Reports[0].Quota.CreditsUsd == nil || again.Reports[0].Quota.CreditsUsd.Percent != 50 {
		t.Fatalf("credits aliased: %#v", again.Reports[0].Quota.CreditsUsd)
	}
	if again.Reports[0].Entitlement == nil || again.Reports[0].Entitlement.PlanRenewsAt != 9999 {
		t.Fatalf("entitlement aliased: %#v", again.Reports[0].Entitlement)
	}
}

func TestNilStoreSnapshotIsEmpty(t *testing.T) {
	var store *Store
	got := store.Snapshot()
	if got.GeneratedAt != 0 || len(got.Reports) != 0 {
		t.Fatalf("snapshot=%#v", got)
	}
}
