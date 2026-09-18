package codexauth

import (
	"testing"
	"time"
)

func TestQuotaStateCredentialGenerationControlsSelectionVisibility(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	usage := 18.0
	if !state.SetParsedForCredential("acct", 4, QuotaReading{WeeklyPercent: &usage}, 1, now) {
		t.Fatal("generation-tagged quota write rejected")
	}

	live := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 4, Credential: &ManagedCredential{}},
	}}
	visible := state.SelectionSnapshotsForCredentials(live)
	if visible["acct"] == nil || visible["acct"].WeeklyPercent == nil || *visible["acct"].WeeklyPercent != 18 {
		t.Fatalf("matching generation hidden: %#v", visible)
	}

	live.Records["acct"] = ManagedCredentialRecord{Generation: 5, Credential: &ManagedCredential{}}
	if stale := state.SelectionSnapshotsForCredentials(live)["acct"]; stale != nil {
		t.Fatalf("stale generation remained selector-visible: %#v", stale)
	}
}

func TestQuotaStateNewCredentialGenerationDoesNotInheritOldEvidence(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly, credits := 10.0, 20.0, 3.0
	state.SetParsedForCredential("acct", 1, QuotaReading{
		WeeklyPercent: &weekly, MonthlyPercent: &monthly, ResetCredits: &credits,
	}, 1, now)

	newWeekly := 35.0
	state.SetParsedForCredential("acct", 2, QuotaReading{WeeklyPercent: &newWeekly}, 1, now.Add(time.Minute))
	got := state.GetForCredential("acct", 2)
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 35 {
		t.Fatalf("new generation quota=%#v", got)
	}
	if got.MonthlyPercent != nil || got.ResetCredits != nil {
		t.Fatalf("new credential inherited old quota evidence: %#v", got)
	}
	if old := state.GetForCredential("acct", 1); old != nil {
		t.Fatalf("old generation remained addressable: %#v", old)
	}
}

func TestQuotaStateRejectsLateOlderCredentialGeneration(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	newUsage := 20.0
	if !state.SetParsedForCredential("acct", 2, QuotaReading{WeeklyPercent: &newUsage}, 1, now) {
		t.Fatal("newer generation write rejected")
	}
	oldUsage := 90.0
	if state.SetParsedForCredential("acct", 1, QuotaReading{WeeklyPercent: &oldUsage}, 1, now.Add(time.Second)) {
		t.Fatal("late older generation quota overwrote newer state")
	}
	got := state.GetForCredential("acct", 2)
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 20 {
		t.Fatalf("newer quota lost after stale write: %#v", got)
	}
}

func TestQuotaStateSameCredentialGenerationKeepsExistingMergeRules(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly := 10.0, 20.0
	state.SetParsedForCredential("acct", 7, QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly, MonthlyIsPrimaryWindow: true}, 1, now)

	updatedWeekly := 40.0
	state.SetParsedForCredential("acct", 7, QuotaReading{WeeklyPercent: &updatedWeekly}, 1, now.Add(time.Minute))
	got := state.GetForCredential("acct", 7)
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 40 || got.MonthlyPercent == nil || *got.MonthlyPercent != 20 || !got.MonthlyIsPrimaryWindow {
		t.Fatalf("same-generation merge changed: %#v", got)
	}
}

func TestQuotaStateGenerationMetadataIsDefensivelyCopied(t *testing.T) {
	state := NewQuotaState()
	usage := 12.0
	state.SetParsedForCredential("acct", 9, QuotaReading{WeeklyPercent: &usage}, 1, time.Now())
	got := state.Get("acct")
	if got == nil || got.CredentialGeneration == nil || *got.CredentialGeneration != 9 {
		t.Fatalf("quota=%#v", got)
	}
	*got.CredentialGeneration = 99
	again := state.Get("acct")
	if again == nil || again.CredentialGeneration == nil || *again.CredentialGeneration != 9 {
		t.Fatalf("generation mutated through defensive copy: %#v", again)
	}
}
