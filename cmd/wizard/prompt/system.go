package prompt

const systemPrefix = "run /system-planner for current workspace, and output under <workspace>/plans/"

// System returns the system-planner prompt template.
func System() Template {
	return Template{
		Flag:   "system-planner",
		Help:   "wrap the process with the system-planner prompt prefix",
		Prefix: systemPrefix,
	}
}
