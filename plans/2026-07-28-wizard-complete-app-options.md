# Wizard Supported App Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `pm2 wizard` collect the operational options users choose while leaving config and log path fields to PM2's existing defaults.

**Architecture:** Keep generic input parsing in `config/wizard/prompt.go`, keep the main prompt sequence in `config/wizard/wizard.go`, and place the three additional operational prompts in `config/wizard/app_options.go`. Keep all supported fields in the renderer so existing ecosystem files with custom paths survive merges, while new wizard answers obtain path defaults through `AppConfig.Normalize`.

**Tech Stack:** Go 1.26.3, Cobra, standard-library tests, embedded goja ecosystem loader.

## Global Constraints

- Preserve the existing first eight prompts: namespace, name, script, args, instances, watch mode, env, cron.
- Add exactly three operational fields after cron: `cron_restart`, `max_restarts`, and `cwd`.
- Do not prompt for `config_dir`, `log_file`, `out_file`, or `error_file`.
- Keep `optional` as the final per-app prompt, followed by add-another-app and write-to-file.
- Default `cron_restart` to blank.
- Default `max_restarts` to `15`.
- Default `cwd` to omission, which the loader resolves to the ecosystem file directory.
- Let normalization derive `config_dir`, `log_file`, and `error_file`; leave `out_file` blank.
- Omit those four keys when they carry default values in newly generated files.
- Preserve custom config and log path fields when rendering existing ecosystem apps.
- Do not expose runtime-managed fields: `version`, `config_file`, `base_env`, or `paused`.
- Do not add unsupported Node.js PM2 fields such as `autorestart`.
- Preserve generated names as uppercase `NAMESPACE SCRIPT - NAME`.
- Do not change `pm2 wizard install`.
- Do not create a commit or pull request.

---

### Task 1: Lock the complete prompt contract with failing tests

**Files:**

- Modify: `config/wizard/wizard_test.go`
- Modify: `cmd/wizard/wizard_test.go`

**Interfaces:**

- Consumes: `collectAnswers`, `DefaultApp`, `renderEcosystemJS`, and the Cobra wizard command.
- Produces: regression coverage for the selected fields, default-managed paths, prompt order, rendering, and `--yes`.

- [x] **Step 1: Add an explicit-options collection test**

Feed values for the three new prompts and assert the resulting `AppConfig`
preserves `CronRestart`, `MaxRestarts`, and `CWD` while normalization supplies
the config and log defaults.

- [x] **Step 2: Add a defaults collection test**

Press Enter through every new prompt and assert `MaxRestarts == 15`, blank
`CWD`, the derived config/log/error paths, and blank `CronRestart` and
`OutFile`.

- [x] **Step 3: Extend prompt-order and renderer tests**

Assert the three labels appear between cron and optional and the four
default-managed path labels never appear. Render a fully populated app and
assert every supported field uses the correct ecosystem key. Round-trip the
JS output through `config.Load`.

- [x] **Step 4: Update existing scripted wizard inputs**

Insert three answer lines into every existing per-app fixture so add-another
and write confirmation answers retain their intended positions.

- [x] **Step 5: Verify RED**

Run:

```bash
go test ./config/wizard ./cmd/wizard -count=1
```

Expected: failure until the three new prompts exist and the four
default-managed path prompts are absent.

### Task 2: Implement missing option prompts and rendering

**Files:**

- Create: `config/wizard/app_options.go`
- Modify: `config/wizard/prompt.go`
- Modify: `config/wizard/wizard.go`
- Modify: `config/wizard/renderer.go`

**Interfaces:**

- Consumes: `process.AppConfig`, `process.NormalizeName`, `promptLine`, and `resolveCronSchedule`.
- Produces: `promptAdditionalAppOptions(*bufio.Reader, io.Writer, *process.AppConfig) error` plus a reusable positive-integer prompt.

- [x] **Step 1: Add a reusable positive-integer prompt**

Generalize the existing bounded instances input so both `instances` and
`max_restarts` accept a positive integer, retry three times, and fall back to
their displayed defaults.

- [x] **Step 2: Add the three app-option prompts**

Collect cron restart, max restarts, and cwd in the global order. Leave config
and log paths unset until the existing normalization step supplies defaults.

- [x] **Step 3: Wire the option block before optional**

Call `promptAdditionalAppOptions` immediately after `cron`, then retain the
existing optional choice menu.

- [x] **Step 4: Render every newly collected field**

Keep renderer support for `cron_restart`, `max_restarts`, `cwd`, `config_dir`,
`log_file`, `out_file`, and `error_file` in canonical snake_case so custom
existing values remain round-trippable.

- [x] **Step 5: Format and verify GREEN**

Run:

```bash
gofmt -w config/wizard/app_options.go config/wizard/prompt.go \
  config/wizard/wizard.go config/wizard/renderer.go \
  config/wizard/wizard_test.go cmd/wizard/wizard_test.go
go test ./config/wizard ./cmd/wizard -count=1
```

Expected: both packages pass.

### Task 3: Refresh the user-facing contract

**Files:**

- Modify: `cmd/wizard/wizard.go`
- Modify: `skills/pm2/SKILL.md`
- Modify: `skills/pm2/references/ecosystem.config.js`

**Interfaces:**

- Consumes: the completed prompt sequence and repository AppConfig schema.
- Produces: matching command help and PM2 skill documentation.

- [x] **Step 1: Update wizard help**

List the three additional fields in their actual prompt order and explain that
config and log paths follow PM2 defaults without prompts.

- [x] **Step 2: Update the PM2 skill**

Document the complete prompt flow, defaults, runtime-only exclusions, and the
fact that `autorestart` is unsupported.

- [x] **Step 3: Correct the annotated ecosystem reference**

Remove the stale unsupported `autorestart` examples and align the field list
with `process.AppConfig`.

### Task 4: Verify the complete change

**Files:**

- Verify all modified source, tests, docs, and this plan.

**Interfaces:**

- Consumes: the completed implementation.
- Produces: fresh focused, full-suite, race, build, vet, help, round-trip, and diff evidence.

- [x] **Step 1: Run focused and full tests**

```bash
go test ./config/wizard ./cmd/wizard -count=1
go test ./... -count=1
go test -race ./... -count=1
```

- [x] **Step 2: Build, vet, and inspect help**

```bash
go build ./...
go vet ./...
go run . wizard --help
```

- [x] **Step 3: Exercise a real generated JS file**

Run the built wizard with `--yes --force` against a temporary output path,
then load that file through `pm2 task start` parsing coverage or the existing
round-trip test without starting a daemon.

- [x] **Step 4: Review the working tree**

```bash
git diff --check
git status --short
git diff --stat
```

Expected: no whitespace errors and only the intended wizard, skill, and plan
files changed.
