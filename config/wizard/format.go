package wizard

import "fmt"

const (
	FormatJS   = "js"
	FormatJSON = "json"
)

func normalizeFormat(format string) (string, error) {
	if format == "" {
		format = defaultFormat
	}
	if format != FormatJS && format != FormatJSON {
		return "", fmt.Errorf("invalid --format %q (want js|json)", format)
	}
	return format, nil
}

func defaultOutputFor(format string) string {
	if format == FormatJSON {
		return "ecosystem.config.json"
	}
	return defaultOutput
}
