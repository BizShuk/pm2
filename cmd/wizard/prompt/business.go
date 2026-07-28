package prompt

const businessPrefix = "run /business-planner for current workspace, and output under <workspace>/plans/"

// Business returns the business-planner prompt template.
func Business() Template {
	return Template{
		Flag:   "business-planner",
		Help:   "wrap the process with the business-planner prompt prefix",
		Prefix: businessPrefix,
	}
}
