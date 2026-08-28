// Package wizard owns ecosystem configuration authoring: prompts, the
// generated app and workflow declarations, merge policy, and JS/JSON
// rendering.
//
// Its unit of work is Ecosystem — one file's `apps:` and `workflows:`
// blocks together — because a task stage references an app, so neither
// block can be collected, merged, or validated on its own.
//
// Cobra and terminal detection stay in package cmd (cmd/wizard.go). This
// package accepts all input and output through WizardContext.
package wizard
