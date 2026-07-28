package prompt

import "testing"

func TestPlannerTemplates(t *testing.T) {
	tests := []struct {
		name   string
		got    Template
		flag   string
		help   string
		prefix string
	}{
		{
			name:   "system",
			got:    System(),
			flag:   "system-planner",
			help:   "wrap the process with the system-planner prompt prefix",
			prefix: "run /system-planner for current workspace, and output under <workspace>/plans/",
		},
		{
			name:   "business",
			got:    Business(),
			flag:   "business-planner",
			help:   "wrap the process with the business-planner prompt prefix",
			prefix: "run /business-planner for current workspace, and output under <workspace>/plans/",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got.Flag != test.flag {
				t.Fatalf("Flag = %q, want %q", test.got.Flag, test.flag)
			}
			if test.got.Help != test.help {
				t.Fatalf("Help = %q, want %q", test.got.Help, test.help)
			}
			if test.got.Render("") != test.prefix {
				t.Fatalf("Render(\"\") = %q, want %q", test.got.Render(""), test.prefix)
			}
			if want := test.prefix + " analyze repo"; test.got.Render("analyze repo") != want {
				t.Fatalf("Render(user prompt) = %q, want %q", test.got.Render("analyze repo"), want)
			}
		})
	}
}

func TestPlannerTemplateFactoriesReturnValues(t *testing.T) {
	first := System()
	first.Flag = "changed"

	if second := System(); second.Flag != "system-planner" {
		t.Fatalf("System() retained caller mutation: %q", second.Flag)
	}
}
