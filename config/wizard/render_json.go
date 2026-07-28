package wizard

import (
	"encoding/json"

	"github.com/bizshuk/pm2/process"
)

func renderEcosystemJSON(apps []process.AppConfig) (string, error) {
	renderedApps := make([]renderedApp, len(apps))
	for i, app := range apps {
		renderedApps[i] = appForRender(app)
	}
	document := struct {
		Apps []renderedApp `json:"apps"`
	}{Apps: renderedApps}

	data, err := json.MarshalIndent(document, "", "    ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
