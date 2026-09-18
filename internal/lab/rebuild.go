package lab

import (
	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/compat"
	"github.com/Wibias/Benes/internal/config"
)

type Result struct {
	EventCount   int
	SubjectCount int
	VerdictCount int
}

func Rebuild(dir string, disk config.DiskConfig, now int64) (Result, error) {
	subjects := SubjectsFromConfig(disk)
	store := NewStore(dir)
	if err := store.Append(Event{EventKind: KindRunStarted, RecordedAt: now, ProducerVersion: compat.SpecVersion}); err != nil {
		return Result{}, err
	}
	verdicts := 0
	for _, subject := range subjects {
		for _, harness := range compat.Harnesses {
			verdict := compat.Classify(harness, subject.Adapter, catalog.CapabilityUnknown)
			if err := store.Append(Event{
				EventKind:       KindVerdictRecorded,
				RecordedAt:      now,
				ProducerVersion: compat.SpecVersion,
				SubjectID:       subject.ID,
				EvidenceLayer:   "protocol_conformance",
				SuiteID:         harness,
				Verdict:         verdict,
			}); err != nil {
				return Result{}, err
			}
			verdicts++
		}
	}
	events, err := store.All()
	if err != nil {
		return Result{}, err
	}
	return Result{EventCount: len(events), SubjectCount: len(subjects), VerdictCount: verdicts}, nil
}
