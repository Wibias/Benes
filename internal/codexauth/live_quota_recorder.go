package codexauth

import (
	"fmt"
	"strings"
	"time"
)

type MainUpstreamQuotaObserver interface {
	ObserveUpstreamQuota(string, QuotaReading) bool
}

type PoolQuotaRecorderConfig struct {
	Quotas    *QuotaState
	MainQuota MainUpstreamQuotaObserver
	Now       func() time.Time
}

type PoolQuotaRecorder struct {
	quotas    *QuotaState
	mainQuota MainUpstreamQuotaObserver
	now       func() time.Time
}

func NewPoolQuotaRecorder(config PoolQuotaRecorderConfig) (*PoolQuotaRecorder, error) {
	if config.Quotas == nil {
		return nil, fmt.Errorf("Codex Pool quota state is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &PoolQuotaRecorder{quotas: config.Quotas, mainQuota: config.MainQuota, now: config.Now}, nil
}

func (r *PoolQuotaRecorder) Record(credential PoolCredential, headers UpstreamQuotaHeaders) bool {
	accountID := strings.TrimSpace(credential.AccountID)
	if accountID == "" {
		return false
	}
	reading := ParseUpstreamQuotaHeaders(headers)
	if reading == nil {
		return false
	}
	if accountID == MainAccountID {
		if r.mainQuota == nil || strings.TrimSpace(credential.MainIdentity) == "" {
			return false
		}
		return r.mainQuota.ObserveUpstreamQuota(credential.MainIdentity, *reading)
	}
	if credential.Generation < 0 {
		return false
	}
	return r.quotas.SetParsedForCredential(
		accountID,
		credential.Generation,
		*reading,
		credential.WriterGeneration,
		r.now(),
	)
}
