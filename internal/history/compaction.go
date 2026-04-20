package history

import "strings"

type CompactionDetails struct {
	ReadFiles     []string `json:"read_files,omitempty"`
	ModifiedFiles []string `json:"modified_files,omitempty"`
}

type CompactionPayload struct {
	Summary      string            `json:"summary"`
	FirstKeptSeq uint64            `json:"first_kept_seq"`
	TokensBefore int               `json:"tokens_before"`
	Reason       string            `json:"reason,omitempty"`
	Details      CompactionDetails `json:"details,omitempty"`
}

type CompactionState struct {
	Envelope EventEnvelope
	Payload  CompactionPayload
	Index    int
}

func LatestCompaction(events []EventEnvelope) (CompactionState, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if strings.TrimSpace(events[i].Kind) != "session.compaction" {
			continue
		}
		return CompactionState{
			Envelope: events[i],
			Payload:  DecodePayload[CompactionPayload](events[i].Payload),
			Index:    i,
		}, true
	}
	return CompactionState{}, false
}

func ActiveEvents(events []EventEnvelope) []EventEnvelope {
	state, ok := LatestCompaction(events)
	if !ok {
		out := make([]EventEnvelope, len(events))
		copy(out, events)
		return out
	}

	start := state.Index + 1
	if idx := indexBySeq(events, state.Payload.FirstKeptSeq); idx >= 0 {
		start = idx
	}
	if start < 0 {
		start = 0
	}
	if start > len(events) {
		start = len(events)
	}

	out := make([]EventEnvelope, 0, len(events)-start)
	for i := start; i < len(events); i++ {
		if i == state.Index {
			continue
		}
		out = append(out, events[i])
	}
	return out
}

func VisibleEvents(events []EventEnvelope) []EventEnvelope {
	state, ok := LatestCompaction(events)
	if !ok {
		out := make([]EventEnvelope, len(events))
		copy(out, events)
		return out
	}

	active := ActiveEvents(events)
	out := make([]EventEnvelope, 0, len(active)+1)
	out = append(out, state.Envelope)
	out = append(out, active...)
	return out
}

func indexBySeq(events []EventEnvelope, seq uint64) int {
	if seq == 0 {
		return -1
	}
	for i, ev := range events {
		if ev.Seq == seq {
			return i
		}
	}
	return -1
}
