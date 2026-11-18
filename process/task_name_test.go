package process

import "testing"

func TestValidateTaskName(t *testing.T) {
	valid := []string{"Classify Photo - Daily", "LLM Proxy - Always", "Unit Tests - On Demand", "3DSimulator Build - Hourly"}
	invalid := []string{"api", "ocr_agent Classify Photo - Daily", "Classify photo - Daily", "Classify Photo - Yearly", "Classify Photo-Daily"}
	for _, n := range valid {
		if err := ValidateTaskName(n); err != nil {
			t.Errorf("ValidateTaskName(%q) = %v, want nil", n, err)
		}
	}
	for _, n := range invalid {
		if ValidateTaskName(n) == nil {
			t.Errorf("ValidateTaskName(%q) = nil, want error", n)
		}
	}
}

func TestFormatTaskName(t *testing.T) {
	cases := []struct {
		purpose string
		app     AppConfig
		want    string
	}{
		{"daily sync", AppConfig{Cron: "0 6 * * *"}, "Daily Sync - Daily"},
		{"clean-cache", AppConfig{Cron: "0 10 * * 5"}, "Clean Cache - Weekly"},
		{"poll", AppConfig{Cron: "*/5 * * * *"}, "Poll - Hourly"},
		{"refresh", AppConfig{Cron: "37 0 1 * *"}, "Refresh - Monthly"},
		{"proxy", AppConfig{}, "Proxy - Always"},
		{"planner", AppConfig{Optional: true}, "Planner - On Demand"},
	}
	for _, c := range cases {
		if got := FormatTaskName(c.purpose, c.app); got != c.want {
			t.Errorf("FormatTaskName(%q) = %q, want %q", c.purpose, got, c.want)
		}
	}
}
