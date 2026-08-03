package sysmon

import "runtime"

// fallbackSampler serves platforms with no implementation. It reports the
// little Go itself knows and an explicit error, so a renderer can show
// "—" for the unavailable readings instead of a convincing-looking zero.
type fallbackSampler struct{}

func newFallbackSampler() *fallbackSampler { return &fallbackSampler{} }

func (f *fallbackSampler) sample() (System, error) {
	return System{CPU: CPU{Cores: runtime.NumCPU()}}, ErrUnsupportedPlatform
}

func (f *fallbackSampler) host() (Host, error) {
	return Host{Cores: runtime.NumCPU()}, ErrUnsupportedPlatform
}
