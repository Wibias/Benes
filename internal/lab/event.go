package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	KindRunStarted      = "lab.run.started"
	KindVerdictRecorded = "lab.verdict.recorded"
	ProducerName        = "benes-lab"
)

type Event struct {
	EventID         string  `json:"eventId"`
	EventKind       string  `json:"eventKind"`
	Sequence        uint64  `json:"sequence"`
	RecordedAt      int64   `json:"recordedAt"`
	Producer        string  `json:"producer"`
	ProducerVersion string  `json:"producerVersion"`
	SubjectID       string  `json:"subjectId,omitempty"`
	EvidenceLayer   string  `json:"evidenceLayer,omitempty"`
	SuiteID         string  `json:"suiteId,omitempty"`
	Verdict         string  `json:"verdict,omitempty"`
	Excluded        bool    `json:"excluded"`
	ExclusionReason *string `json:"exclusionReason"`
	PrevHash        string  `json:"prevHash"`
	EventHash       string  `json:"eventHash"`
}

func HashEvent(ev Event) (string, error) {
	copy := ev
	copy.EventHash = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func newEventID(seq uint64, recordedAt int64) string {
	return fmt.Sprintf("%d-%d", recordedAt, seq)
}
