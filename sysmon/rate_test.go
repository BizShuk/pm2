package sysmon

import (
	"testing"
	"time"
)

func TestRateTracker(t *testing.T) {
	tracker := newRateTracker()
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	if got := tracker.rate("eth0", 1000, start); got != 0 {
		t.Errorf("first observation = %v, want 0 — reporting the counter itself would show everything since boot as one second of traffic", got)
	}
	if got := tracker.rate("eth0", 3000, start.Add(2*time.Second)); got != 1000 {
		t.Errorf("rate = %v, want 2000 bytes over 2s", got)
	}
	if got := tracker.rate("eth0", 3500, start.Add(3*time.Second)); got != 500 {
		t.Errorf("rate = %v, want 500 bytes over 1s", got)
	}
}

func TestRateTrackerResetsOnCounterRegression(t *testing.T) {
	// A device that disappears and returns, a counter wrap, or a
	// container restart all move the counter backwards. Reporting a
	// negative or wrapped rate would spike the graph.
	tracker := newRateTracker()
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	tracker.rate("sda", 5000, start)
	if got := tracker.rate("sda", 100, start.Add(time.Second)); got != 0 {
		t.Errorf("rate after regression = %v, want 0", got)
	}
	if got := tracker.rate("sda", 300, start.Add(2*time.Second)); got != 200 {
		t.Errorf("rate = %v, want the new baseline to take effect immediately", got)
	}
}

func TestRateTrackerIgnoresNonAdvancingClock(t *testing.T) {
	tracker := newRateTracker()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	tracker.rate("sda", 1000, now)
	if got := tracker.rate("sda", 2000, now); got != 0 {
		t.Errorf("rate = %v, want 0 rather than a division by zero", got)
	}
}

func TestRateTrackerKeepsKeysIndependent(t *testing.T) {
	tracker := newRateTracker()
	start := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	tracker.rate("eth0/rx", 1000, start)
	tracker.rate("eth0/tx", 100, start)

	if got := tracker.rate("eth0/rx", 2000, start.Add(time.Second)); got != 1000 {
		t.Errorf("rx rate = %v, want 1000", got)
	}
	if got := tracker.rate("eth0/tx", 200, start.Add(time.Second)); got != 100 {
		t.Errorf("tx rate = %v, want 100 — rx must not contaminate tx", got)
	}
}
