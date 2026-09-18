package lab

import (
	"errors"
	"sort"
)

type Projection struct {
	Events          []Event
	Verdicts        map[string]Event
	Observations    []Event
	CorruptionCount int
}

func VerdictKey(subjectID, suiteID string) string {
	return subjectID + "\x00" + suiteID
}

func LoadProjection(dir string) (Projection, error) {
	store := NewStore(dir)
	events, err := store.All()
	if err != nil {
		if errors.Is(err, ErrCorrupt) {
			return Projection{CorruptionCount: 1}, nil
		}
		return Projection{}, err
	}
	verdicts := make(map[string]Event)
	for _, ev := range events {
		if ev.EventKind != KindVerdictRecorded {
			continue
		}
		key := VerdictKey(ev.SubjectID, ev.SuiteID)
		verdicts[key] = ev
	}
	observations := make([]Event, 0, len(verdicts))
	for _, ev := range verdicts {
		observations = append(observations, ev)
	}
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].SubjectID != observations[j].SubjectID {
			return observations[i].SubjectID < observations[j].SubjectID
		}
		return observations[i].SuiteID < observations[j].SuiteID
	})
	if len(observations) > 200 {
		observations = observations[:200]
	}
	return Projection{
		Events:          events,
		Verdicts:        verdicts,
		Observations:    observations,
		CorruptionCount: 0,
	}, nil
}
