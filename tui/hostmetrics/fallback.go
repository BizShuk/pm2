package hostmetrics

// fallbackCollector returns the same cosmetic 5.2 / 64.1 values
// the legacy macOS path used in its parse-failure branch. It is
// the safety net for any host that is not darwin/linux (e.g.
// plan9, js/wasm builds) and for any environment where the
// platform-specific collector returns an error and the caller
// chose to fall back rather than surface the error.
//
// The numbers are deliberately stable: a TUI that flickers
// between a real value and an error state is harder to read than
// a stable cosmetic one.
type fallbackCollector struct{}

func newFallbackCollector() *fallbackCollector { return &fallbackCollector{} }

func (f *fallbackCollector) Collect() (float64, float64, error) {
	return 5.2, 64.1, nil
}
