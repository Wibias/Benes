package antigravity

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPoolSkipsCooledAccountsAndBindsProjectToSameSnapshot(t *testing.T) {
	pool := NewPool()
	accounts := []Account{
		{ID: "a", Token: "ta", ProjectID: "pa"},
		{ID: "b", Token: "tb", ProjectID: "pb"},
	}
	first, err := pool.Select(accounts)
	if err != nil || first.ID != "a" || first.ProjectID != "pa" {
		t.Fatalf("first=%#v %v", first, err)
	}
	pool.Mark(first.ID, FailureGeoblock, time.Minute)
	second, err := pool.Select(accounts)
	if err != nil || second.ID != "b" || second.ProjectID != "pb" || second.ProjectID == first.ProjectID {
		t.Fatalf("rotation=%#v %v", second, err)
	}
	pool.Mark(second.ID, FailureQuota, time.Minute)
	if _, err := pool.Select(accounts); !errors.Is(err, ErrNoUsableAccount) {
		t.Fatalf("exhausted=%v", err)
	}
}

func TestClassifyFailures(t *testing.T) {
	if ClassifyStatus(403, `{"error":{"status":"FAILED_PRECONDITION"}}`, false) != FailureGeoblock {
		t.Fatal("geoblock")
	}
	if ClassifyStatus(429, "quota exhausted", false) != FailureQuota {
		t.Fatal("quota")
	}
	if ClassifyStatus(429, "rate", false) != FailureRateLimit {
		t.Fatal("rate")
	}
	if ClassifyStatus(403, `{"error":{"status":"VALIDATION_REQUIRED"}}`, false) != FailurePermission {
		t.Fatal("account permission")
	}
	if ClassifyStatus(403, `{"error":{"status":"PERMISSION_DENIED","message":"project forbidden"}}`, false) == FailurePermission {
		t.Fatal("generic forbidden must not be account-scoped")
	}
}

func TestSelectAllowsMissingProjectForLaterDiscovery(t *testing.T) {
	pool := NewPool()
	got, err := pool.Select([]Account{{ID: "a", Token: "t"}})
	if err != nil || got.ID != "a" || got.ProjectID != "" {
		t.Fatalf("discoverable snapshot=%#v err=%v", got, err)
	}
	if _, err := pool.Select([]Account{{ID: "a"}}); !errors.Is(err, ErrNoUsableAccount) {
		t.Fatalf("missing token=%v", err)
	}
}

func TestParseRetryAfterCapsAndReadsSeconds(t *testing.T) {
	if got := ParseRetryAfter(http.Header{"Retry-After": []string{"120"}}); got != 120*time.Second {
		t.Fatalf("retry-after=%s", got)
	}
}
