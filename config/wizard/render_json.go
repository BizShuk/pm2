package wizard

import "encoding/json"

func renderEcosystemJSON(doc Ecosystem) (string, error) {
	renderedApps := make([]renderedApp, len(doc.Apps))
	for i, app := range doc.Apps {
		renderedApps[i] = appForRender(app)
	}
	var renderedWorkflows []renderedWorkflow
	for _, wf := range doc.Workflows {
		renderedWorkflows = append(renderedWorkflows, workflowForRender(wf))
	}
	document := struct {
		Apps      []renderedApp      `json:"apps"`
		Workflows []renderedWorkflow `json:"workflows,omitempty"`
	}{Apps: renderedApps, Workflows: renderedWorkflows}

	data, err := json.MarshalIndent(document, "", "    ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
