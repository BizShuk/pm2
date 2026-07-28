package prompt

// Template describes one planner flag and its generated prompt prefix.
type Template struct {
	Flag   string
	Help   string
	Prefix string
}

// Render appends a user prompt to the template prefix.
func (template Template) Render(userPrompt string) string {
	if userPrompt == "" {
		return template.Prefix
	}
	return template.Prefix + " " + userPrompt
}
