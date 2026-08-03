package sysmon

import "time"

// rateTracker turns monotonically increasing OS counters into per-second
// rates. Platform samplers keep one tracker per counter family (network,
// disk) and call rate once per sample.
//
// The first observation of a key has nothing to subtract from, so it
// returns 0 — a fresh dashboard shows a zero rate for one tick rather
// than a spike of "everything since boot".
type rateTracker struct {
	previous map[string]counterSample
}

type counterSample struct {
	value uint64
	at    time.Time
}

func newRateTracker() *rateTracker {
	return &rateTracker{previous: make(map[string]counterSample)}
}

// rate records value for key at instant now and returns the per-second
// change since the previous call. A counter that moved backwards (device
// removed, counter wrap, container restart) resets the baseline and
// reports 0 instead of a negative or absurd rate.
func (t *rateTracker) rate(key string, value uint64, now time.Time) float64 {
	previous, seen := t.previous[key]
	t.previous[key] = counterSample{value: value, at: now}
	if !seen || value < previous.value {
		return 0
	}
	elapsed := now.Sub(previous.at).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(value-previous.value) / elapsed
}
