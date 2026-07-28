# Wizard Namespace Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update `pm2 wizard` so its prompts follow the requested order, use fixed namespace and optional menus, support `r` as a random daily cron time from 02:00 through 08:00, and end with add-another-app and write-to-file confirmations.

**Architecture:** Keep interactive orchestration in `config/wizard/wizard.go` and add reusable menu and cron-input helpers to the existing prompt-focused `config/wizard/prompt.go`. Preserve `process.AppConfig` and renderer boundaries; the Cobra layer only updates its user-facing wizard description and non-terminal error text.

**Tech Stack:** Go 1.26.3, Cobra, standard-library `math/rand/v2`, table-driven Go tests.

## Global Constraints

- Per-app prompt order is exactly: namespace, name, script, args, instances, watch mode, env, cron schedule, optional.
- Namespace choices are exactly `Agent`, `Backup`, `Local`, `Service`, and `AutoP`, in that order.
- Blank namespace input selects option 1, `Agent`; `--yes` generates the same default.
- Blank cron input skips scheduling.
- Cron input `r`, case-insensitively, becomes one concrete daily five-field cron expression in the inclusive 02:00-08:00 window.
- Any other cron input is retained as the customized cron expression.
- Optional choice 1 is `Yes (registered but paused)` and sets `optional: true`; choice 2 is `No` and sets `optional: false`.
- The cron question does not add a separate `cron_restart` question.
- After each app, prompt to add another app; after the preview, prompt to write to the selected file.
- The same buffered input reader must carry unread answers from app collection into the final write confirmation.
- Do not change `pm2 wizard install`.

---

### Task 1: Lock the interactive prompt contract with failing tests

**Files:**

- Modify: `config/wizard/wizard_test.go`
- Modify: `cmd/wizard/wizard_test.go`

**Interfaces:**

- Consumes: `collectAnswers(io.Reader, io.Writer)` and the Cobra-facing `runWizard` test helper.
- Produces: regression coverage for field values, prompt ordering, displayed menu text, cron shortcut behavior, and final write wording.

- [x] **Step 1: Add the namespace and prompt-order test**

Drive one app with:

```go
input := strings.Join([]string{
    "5", "worker", "./worker.js", "--queue high", "2", "y",
    "MODE=prod", "", "0 4 * * *", "2", "n",
}, "\n") + "\n"
```

Assert the resulting app has namespace `AutoP`, name `worker`, script
`./worker.js`, two arguments, two instances, watch enabled, the environment
variable, the custom cron, and `Optional == false`. Assert these output
fragments appear in order:

```text
Namespace:
Name [app]:
Script [app.js]:
Args (space-separated):
Instances [1]:
Watch mode? [y/N]:
Env vars?
Cron schedule (blank to skip, r for random daily between 2am and 8am, or enter cron format):
Optional:
Add another app? [y/N]:
```

- [x] **Step 2: Add menu/default and random-cron tests**

Assert namespace menu options render in the exact requested order, blank input
selects `Agent`, optional option 1 sets `Optional`, optional option 2 clears it,
custom cron survives unchanged, and the random shortcut returns a five-field
daily schedule whose hour/minute lies between 02:00 and 08:00 inclusive.

- [x] **Step 3: Add the CLI prompt-copy test**

Run the Cobra wizard through `runWizard`, answer the final confirmation with
`n`, and assert stdout contains:

```text
Write to file <path>? [Y/n]:
Aborted.
```

Assert no file is written, proving that the final answer is not lost when the
app collector buffers ahead on a non-terminal test reader.

Also assert the command's long help names the prompt order, namespace choices,
random cron shortcut, and optional-paused semantics.

- [x] **Step 4: Run focused tests and verify RED**

Run:

```bash
go test ./config/wizard ./cmd/wizard -run 'Test(CollectAnswersUsesRequestedPromptFlow|WizardChoiceMenus|ResolveCronSchedule|WizardPromptCopy)' -count=1
```

Expected: FAIL because the namespace remains a free-text fourth prompt, `r`
is not resolved, optional is still y/n, the cron-restart prompt still exists,
and final write copy does not say `Write to file`.

### Task 2: Implement the menu-driven wizard flow

**Files:**

- Modify: `config/wizard/prompt.go`
- Modify: `config/wizard/wizard.go`
- Modify: `config/wizard/renderer.go`
- Modify: `cmd/wizard/wizard.go`
- Modify: `skills/pm2/SKILL.md`

**Interfaces:**

- Consumes: existing `promptLine`, `promptYesNo`, `promptInstances`,
  `promptEnvVars`, `process.AppConfig`, and Cobra stream wiring.
- Produces: `promptChoice`, `promptNamespace`, `promptOptional`,
  `resolveCronSchedule`, and the reordered `askOneApp`.

- [x] **Step 1: Add a bounded numeric choice helper**

Add a focused helper that renders numbered labels, defaults blank input to the
specified selection, accepts only in-range numbers, retries invalid input no
more than five times, and returns a clear error when no valid choice is made:

```go
func promptChoice(
    rdr *bufio.Reader,
    out io.Writer,
    label string,
    options []string,
    defaultChoice int,
) (int, error)
```

- [x] **Step 2: Add namespace and optional menus**

Use these exact options:

```go
var namespaceOptions = []string{"Agent", "Backup", "Local", "Service", "AutoP"}
var optionalOptions = []string{"Yes (registered but paused)", "No"}
```

Map the selected namespace directly to `AppConfig.Namespace`; map optional
choice 1 to `true` and choice 2 to `false`.

- [x] **Step 3: Add random daily cron resolution**

For `r`, choose a random minute offset from the 361 possible minute values
between 02:00 and 08:00 inclusive, then render:

```go
fmt.Sprintf("%d %d * * *", minute, hour)
```

Blank remains blank; other trimmed text is returned as the custom expression.

- [x] **Step 4: Reorder `askOneApp`**

Collect fields in the global-constraint order. Use `app` as the blank name
default and `app.js` as the blank script default. Remove the conditional
`Cron restart?` question and leave `CronRestart` empty.

- [x] **Step 5: Preserve the reader through final confirmation**

Create one `*bufio.Reader` in `RunInteractive`, use it for app collection, and
pass that same reader to `WriteEcosystemFile` through `WizardContext.In`.

- [x] **Step 6: Refresh the final prompt and Cobra copy**

Change the write confirmation to:

```go
fmt.Sprintf("Write to file %s?", output)
```

Update `Cmd.Long` with the new flow and replace the stale `pm2 eco requires`
non-terminal message with `pm2 wizard requires`. Mirror the same ordered flow
and choice semantics in the repository PM2 skill reference.

- [x] **Step 7: Format and verify GREEN**

Run:

```bash
gofmt -w config/wizard/prompt.go config/wizard/wizard.go \
  config/wizard/renderer.go config/wizard/wizard_test.go \
  cmd/wizard/wizard.go cmd/wizard/wizard_test.go
go test ./config/wizard ./cmd/wizard -count=1
```

Expected: both packages pass.

### Task 3: Verify the complete wizard behavior

**Files:**

- Verify all modified Go files and this plan.

**Interfaces:**

- Consumes: the completed wizard prompt flow.
- Produces: fresh unit, race, build, help, and diff evidence.

- [x] **Step 1: Run the full test suite**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
```

Expected: all packages pass with zero failures and no race reports.

- [x] **Step 2: Build and inspect user-facing help**

Run:

```bash
go build ./...
go run . wizard --help
```

Expected: both commands exit successfully; wizard help describes the requested
prompt order and choices.

- [x] **Step 3: Exercise the real interactive path**

Build a temporary binary and feed one complete answer set through a pseudo-TTY
or a focused Cobra integration test. Confirm the written ecosystem file has
the selected namespace, optional value, and concrete random/custom cron value.

- [x] **Step 4: Review the working tree**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Expected: no whitespace errors and only the intended wizard files plus this
plan are changed.

### Task 4: Format wizard-generated app names

**Files:**

- Modify: `config/wizard/wizard_test.go`
- Modify: `cmd/wizard/wizard_test.go`
- Modify: `config/wizard/wizard.go`
- Modify: `cmd/wizard/wizard.go`
- Modify: `skills/pm2/SKILL.md`

**Interfaces:**

- Consumes: the selected namespace, script path, and entered name.
- Produces: `formatWizardName(namespace, script, name string) string` and
  wizard-generated `AppConfig.Name` values in the exact uppercase form
  `NAMESPACE SCRIPT - NAME`.

- [x] **Step 1: Write failing naming tests**

Assert that namespace `AutoP`, script `./worker.js`, and entered name
`daily sync` become:

```text
AUTOP WORKER - DAILY SYNC
```

Assert the `--yes` defaults become `AGENT APP - APP`, and the Cobra end-to-end
output uses the composed name rather than the raw entered name.

- [x] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./config/wizard ./cmd/wizard \
  -run 'Test(CollectAnswers|RunInteractiveYesAll|WizardEndToEndMerge)' -count=1
```

Expected: FAIL because the wizard still writes the raw name.

- [x] **Step 3: Implement name composition**

Build the script component with `DeriveName(script)`, join the three
components with:

```go
fmt.Sprintf("%s %s - %s", namespace, scriptName, name)
```

Then uppercase the complete result. Apply it only to interactive wizard apps,
including `--yes`; leave `pm2 wizard install` naming unchanged.

- [x] **Step 4: Synchronize prompt guidance**

State the exact `NAMESPACE SCRIPT - NAME` convention in Cobra long help and
the repository PM2 skill reference.

- [x] **Step 5: Verify GREEN and the complete repository**

Run:

```bash
gofmt -w config/wizard/wizard.go config/wizard/wizard_test.go \
  cmd/wizard/wizard.go cmd/wizard/wizard_test.go
go test ./... -count=1
go test -race ./... -count=1
go build ./...
go vet ./...
git diff --check
```

Expected: every command exits successfully and the existing untracked root
`ecosystem.config.js` remains untouched.
