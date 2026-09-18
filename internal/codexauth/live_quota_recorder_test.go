package codexauth

import (
	"context"
	"testing"
	"time"
)

type fakeMainUpstreamQuotaObserver struct {
	identity string
	reading  QuotaReading
	calls    int
	accept   bool
}

func (f *fakeMainUpstreamQuotaObserver) ObserveUpstreamQuota(identity string, reading QuotaReading) bool {
	f.identity = identity
	f.reading = reading
	f.calls++
	return f.accept
}

func TestPoolQuotaRecorderPublishesManagedQuotaWithCredentialGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := NewQuotaState()
	recorder, err := NewPoolQuotaRecorder(PoolQuotaRecorderConfig{Quotas: quotas, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if !recorder.Record(PoolCredential{AccountID: "managed", Generation: 3, WriterGeneration: 8}, UpstreamQuotaHeaders{
		PrimaryUsedPercent: "45", TertiaryUsedPercent: "12",
	}) {
		t.Fatal("managed live quota rejected")
	}
	quota := quotas.GetForCredential("managed", 3)
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 45 || quota.MonthlyPercent == nil || *quota.MonthlyPercent != 12 {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestPoolQuotaRecorderCannotOverwriteNewerManagedGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := NewQuotaState()
	newUsage := 10.0
	if !quotas.SetParsedForCredential("managed", 4, QuotaReading{WeeklyPercent: &newUsage}, 8, now) {
		t.Fatal("new quota write")
	}
	recorder, err := NewPoolQuotaRecorder(PoolQuotaRecorderConfig{Quotas: quotas, Now: func() time.Time { return now.Add(time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if recorder.Record(PoolCredential{AccountID: "managed", Generation: 3, WriterGeneration: 8}, UpstreamQuotaHeaders{PrimaryUsedPercent: "90"}) {
		t.Fatal("late older managed response overwrote newer quota")
	}
	quota := quotas.GetForCredential("managed", 4)
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 10 {
		t.Fatalf("newer quota=%#v", quota)
	}
}

func TestPoolQuotaRecorderRoutesPhysicalMainThroughIdentityFence(t *testing.T) {
	main := &fakeMainUpstreamQuotaObserver{accept: true}
	recorder, err := NewPoolQuotaRecorder(PoolQuotaRecorderConfig{Quotas: NewQuotaState(), MainQuota: main})
	if err != nil {
		t.Fatal(err)
	}
	if !recorder.Record(PoolCredential{AccountID: MainAccountID, MainIdentity: "physical-id"}, UpstreamQuotaHeaders{PrimaryUsedPercent: "35", PrimaryWindowMinutes: "40320"}) {
		t.Fatal("main live quota rejected")
	}
	if main.calls != 1 || main.identity != "physical-id" || main.reading.MonthlyPercent == nil || *main.reading.MonthlyPercent != 35 || !main.reading.MonthlyIsPrimaryWindow {
		t.Fatalf("main=%#v", main)
	}
}

func TestPoolQuotaRecorderRejectsMissingIdentityInvalidGenerationAndHeaderlessEvidence(t *testing.T) {
	main := &fakeMainUpstreamQuotaObserver{accept: true}
	recorder, err := NewPoolQuotaRecorder(PoolQuotaRecorderConfig{Quotas: NewQuotaState(), MainQuota: main})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		credential PoolCredential
		headers    UpstreamQuotaHeaders
	}{
		{credential: PoolCredential{}, headers: UpstreamQuotaHeaders{PrimaryUsedPercent: "20"}},
		{credential: PoolCredential{AccountID: "managed", Generation: -1}, headers: UpstreamQuotaHeaders{PrimaryUsedPercent: "20"}},
		{credential: PoolCredential{AccountID: MainAccountID}, headers: UpstreamQuotaHeaders{PrimaryUsedPercent: "20"}},
		{credential: PoolCredential{AccountID: "managed", Generation: 2}, headers: UpstreamQuotaHeaders{}},
	} {
		if recorder.Record(tc.credential, tc.headers) {
			t.Fatalf("unexpected record for %#v", tc)
		}
	}
}

func TestPoolCredentialResolverCarriesPhysicalMainIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	main := &fakeMainCredentialReader{result: MainCredentialResult{
		Status: MainCredentialOK, Identity: "physical-id",
		Credential: MainCredential{AccessToken: "main-access", ChatGPTAccountID: "main-chat"},
	}}
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: selector, ManagedTokens: &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()}, MainCredentials: main,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = main.result
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, WriterGeneration: 9})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != MainAccountID || credential.MainIdentity != "physical-id" {
		t.Fatalf("credential=%#v", credential)
	}
}
