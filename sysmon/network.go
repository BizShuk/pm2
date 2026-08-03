package sysmon

import "time"

// interfaceCounters is one network interface's cumulative byte totals as
// reported by the OS. Every platform sampler reduces its own source
// (`netstat -ib`, /proc/net/dev) to this shape so the rate arithmetic
// stays in one place.
type interfaceCounters struct {
	name string
	rx   uint64
	tx   uint64
}

// networkFrom converts cumulative per-interface counters into aggregate
// rates and names the busiest link. Rates are tracked per interface, not
// on the summed total, so a link disappearing between samples resets only
// its own baseline instead of zeroing the whole machine's throughput.
func networkFrom(tracker *rateTracker, counters []interfaceCounters, now time.Time) Network {
	network := Network{}
	busiest := uint64(0)
	for _, counter := range counters {
		network.RxBytesTotal += counter.rx
		network.TxBytesTotal += counter.tx
		network.RxBytesPerSecond += tracker.rate(counter.name+"/rx", counter.rx, now)
		network.TxBytesPerSecond += tracker.rate(counter.name+"/tx", counter.tx, now)
		if counter.rx+counter.tx > busiest {
			busiest = counter.rx + counter.tx
			network.Interface = counter.name
		}
	}
	return network
}
