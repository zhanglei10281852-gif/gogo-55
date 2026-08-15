package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"releaserail/internal/rail"
)

const usageText = `ReleaseRail - offline release orchestration

Usage:
  releaserail validate [flags] <manifest>
  releaserail plan [flags] <manifest>
  releaserail apply [flags] <manifest>
  releaserail status [flags]
  releaserail verify [flags] <manifest>
  releaserail rollback [flags] <manifest>
  releaserail diff [flags] <before> <after>
  releaserail report [flags]

All operations are local. apply and rollback simulate state transitions and never deploy artifacts.
`

type application struct {
	out io.Writer
	err io.Writer
	now func() time.Time
}

func main() {
	app := application{out: os.Stdout, err: os.Stderr, now: time.Now}
	if err := app.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func (a application) run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(a.out, usageText)
		return nil
	}
	switch args[0] {
	case "validate":
		return a.validate(args[1:])
	case "plan":
		return a.plan(args[1:])
	case "apply":
		return a.apply(args[1:])
	case "status":
		return a.status(args[1:])
	case "verify":
		return a.verify(args[1:])
	case "rollback":
		return a.rollback(args[1:])
	case "diff":
		return a.diff(args[1:])
	case "report":
		return a.report(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func newFlags(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	return flags
}

func requireArgs(flags *flag.FlagSet, count int, description string) error {
	if flags.NArg() != count {
		return fmt.Errorf("%s requires %s", flags.Name(), description)
	}
	return nil
}

func loadManifest(path string) (rail.Manifest, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return rail.Manifest{}, "", err
	}
	manifest, err := rail.LoadManifest(absolute)
	if err != nil {
		return rail.Manifest{}, "", err
	}
	return manifest, filepath.Dir(absolute), nil
}

func printJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (a application) validate(args []string) error {
	flags := newFlags("validate", a.err)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	checkArtifacts := flags.Bool("artifacts", false, "also verify artifact bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 1, "one manifest path"); err != nil {
		return err
	}
	manifest, baseDir, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	result := rail.ValidateManifest(manifest, baseDir)
	artifacts := []rail.ArtifactResult{}
	if *checkArtifacts && result.Valid {
		artifacts = rail.VerifyArtifacts(manifest, baseDir)
		_, invalid, messages := rail.ArtifactSummary(artifacts)
		for _, message := range messages {
			result.Issues = append(result.Issues, rail.ValidationIssue{Path: "artifacts", Severity: "error", Message: message})
		}
		if invalid > 0 {
			result.Valid = false
		}
	}
	if *jsonOutput {
		payload := struct {
			Validation rail.ValidationResult `json:"validation"`
			Artifacts  []rail.ArtifactResult `json:"artifacts,omitempty"`
		}{result, artifacts}
		if err := printJSON(a.out, payload); err != nil {
			return err
		}
	} else {
		if result.Valid {
			fmt.Fprintln(a.out, "manifest is valid")
		}
		for _, issue := range result.Issues {
			fmt.Fprintf(a.out, "%s: %s: %s\n", strings.ToUpper(issue.Severity), issue.Path, issue.Message)
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(a.out, "artifact %s: valid=%t size=%d sha256=%s\n", artifact.Component, artifact.Valid, artifact.Size, artifact.SHA256)
		}
	}
	return result.Error()
}

func (a application) plan(args []string) error {
	flags := newFlags("plan", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	verifyArtifacts := flags.Bool("verify", true, "verify artifacts before planning")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 1, "one manifest path"); err != nil {
		return err
	}
	manifest, baseDir, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	validation := rail.ValidateManifest(manifest, baseDir)
	if err := validation.Error(); err != nil {
		return err
	}
	store := rail.NewStore(*stateDir)
	state, loadErr := store.Load()
	if errors.Is(loadErr, os.ErrNotExist) {
		hash, hashErr := rail.ManifestHash(manifest)
		if hashErr != nil {
			return hashErr
		}
		state = rail.InitializeState(manifest, hash, a.now())
	} else if loadErr != nil {
		return loadErr
	}
	artifacts := []rail.ArtifactResult{}
	if *verifyArtifacts {
		artifacts = rail.VerifyArtifacts(manifest, baseDir)
		_, invalid, messages := rail.ArtifactSummary(artifacts)
		if invalid > 0 {
			return fmt.Errorf("artifact verification failed: %s", strings.Join(messages, "; "))
		}
	}
	plan, err := rail.BuildPlan(manifest, state, artifacts, a.now())
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(a.out, plan)
	}
	renderPlan(a.out, plan)
	return nil
}

func renderPlan(writer io.Writer, plan rail.Plan) {
	fmt.Fprintf(writer, "Release %s\nManifest %s\n", plan.Release, plan.ManifestHash)
	for _, environment := range plan.Environments {
		blocked := environment.Blocked()
		fmt.Fprintf(writer, "\nEnvironment %s (rank %d, blocked=%t)\n", environment.Name, environment.Rank, blocked)
		for _, gate := range environment.Gates {
			fmt.Fprintf(writer, "  gate %s [%s]: satisfied=%t (%s)\n", gate.Name, gate.Kind, gate.Satisfied, gate.Explanation)
		}
		for _, component := range environment.Components {
			fmt.Fprintf(writer, "  %d. %s@%s waves=%d migrations=%d health=%d\n",
				component.Order, component.Name, component.Version, len(component.Waves), len(component.Migrations), len(component.Health))
			for index, wave := range component.Waves {
				fmt.Fprintf(writer, "     wave %d: %v\n", index+1, wave)
			}
		}
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintln(writer, "WARNING:", warning)
	}
}
func (a application) apply(args []string) error {
	flags := newFlags("apply", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	environment := flags.String("env", "", "environment to simulate")
	actor := flags.String("actor", "local-user", "audit actor")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 1, "one manifest path"); err != nil {
		return err
	}
	if *environment == "" {
		return fmt.Errorf("apply requires --env")
	}
	manifest, baseDir, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	validation := rail.ValidateManifest(manifest, baseDir)
	if err := validation.Error(); err != nil {
		return err
	}
	artifacts := rail.VerifyArtifacts(manifest, baseDir)
	_, invalid, messages := rail.ArtifactSummary(artifacts)
	if invalid > 0 {
		return fmt.Errorf("artifact verification failed: %s", strings.Join(messages, "; "))
	}
	store := rail.NewStore(*stateDir)
	lock, err := store.Acquire("apply", 2*time.Second, 10*time.Minute)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, loadErr := store.Load()
	if errors.Is(loadErr, os.ErrNotExist) {
		hash, hashErr := rail.ManifestHash(manifest)
		if hashErr != nil {
			return hashErr
		}
		state = rail.InitializeState(manifest, hash, a.now())
	} else if loadErr != nil {
		return loadErr
	}
	plan, err := rail.BuildPlan(manifest, state, artifacts, a.now())
	if err != nil {
		return err
	}
	next, err := rail.ApplySimulation(plan, state, *environment, a.now())
	if err != nil {
		if next.Release != "" {
			_ = store.Save(next)
		}
		return err
	}
	if err := store.Save(next); err != nil {
		return err
	}
	if _, err := store.AppendAudit("apply", *actor, next.Release, map[string]any{
		"environment": *environment, "simulation": true, "manifestHash": next.ManifestHash,
	}, a.now()); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(a.out, next.Environments[*environment])
	}
	fmt.Fprintf(a.out, "simulated release %s in %s: completed\n", next.Release, *environment)
	return nil
}

func (a application) status(args []string) error {
	flags := newFlags("status", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 0, "no positional arguments"); err != nil {
		return err
	}
	store := rail.NewStore(*stateDir)
	state, err := store.Load()
	if err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(a.out, state)
	}
	fmt.Fprintf(a.out, "Release: %s\nManifest: %s\nUpdated: %s\n", state.Release, state.ManifestHash, state.UpdatedAt.Format(time.RFC3339))
	names := make([]string, 0, len(state.Environments))
	for name := range state.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		environment := state.Environments[name]
		fmt.Fprintf(a.out, "%s: %s\n", name, environment.Status)
		componentNames := make([]string, 0, len(environment.Components))
		for component := range environment.Components {
			componentNames = append(componentNames, component)
		}
		sort.Strings(componentNames)
		for _, componentName := range componentNames {
			component := environment.Components[componentName]
			fmt.Fprintf(a.out, "  %s@%s: %s wave=%d instances=%d\n", componentName, component.Version, component.Status, component.CurrentWave, len(component.CompletedInstances))
		}
	}
	return nil
}

func (a application) verify(args []string) error {
	flags := newFlags("verify", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	auditOnly := flags.Bool("audit-only", false, "verify only the audit chain")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *auditOnly {
		if flags.NArg() != 0 {
			return fmt.Errorf("verify --audit-only accepts no manifest")
		}
		verification, err := rail.NewStore(*stateDir).VerifyAudit()
		if err != nil {
			return err
		}
		if *jsonOutput {
			if err := printJSON(a.out, verification); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(a.out, "audit valid=%t records=%d last=%s\n", verification.Valid, verification.Records, verification.LastHash)
		}
		if !verification.Valid {
			return fmt.Errorf("audit chain invalid at line %d: %s", verification.BrokenLine, verification.Error)
		}
		return nil
	}
	if err := requireArgs(flags, 1, "one manifest path, or --audit-only"); err != nil {
		return err
	}
	manifest, baseDir, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	results := rail.VerifyArtifacts(manifest, baseDir)
	audit, err := rail.NewStore(*stateDir).VerifyAudit()
	if err != nil {
		return err
	}
	payload := struct {
		Artifacts []rail.ArtifactResult  `json:"artifacts"`
		Audit     rail.AuditVerification `json:"audit"`
	}{results, audit}
	if *jsonOutput {
		if err := printJSON(a.out, payload); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			fmt.Fprintf(a.out, "%s: valid=%t size=%d sha256=%s", result.Component, result.Valid, result.Size, result.SHA256)
			if result.Error != "" {
				fmt.Fprintf(a.out, " error=%s", result.Error)
			}
			fmt.Fprintln(a.out)
		}
		fmt.Fprintf(a.out, "audit: valid=%t records=%d\n", audit.Valid, audit.Records)
	}
	_, invalid, messages := rail.ArtifactSummary(results)
	if invalid > 0 {
		return fmt.Errorf("artifact verification failed: %s", strings.Join(messages, "; "))
	}
	if !audit.Valid {
		return fmt.Errorf("audit chain invalid: %s", audit.Error)
	}
	return nil
}
func (a application) rollback(args []string) error {
	flags := newFlags("rollback", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	environment := flags.String("env", "", "environment to roll back")
	previousText := flags.String("previous", "", "comma-separated component=version targets")
	actor := flags.String("actor", "local-user", "audit actor")
	force := flags.Bool("force", false, "allow rollback marked unsafe")
	planOnly := flags.Bool("plan-only", false, "print rollback plan without changing state")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 1, "one manifest path"); err != nil {
		return err
	}
	if *environment == "" {
		return fmt.Errorf("rollback requires --env")
	}
	manifest, _, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	previous, err := parseAssignments(*previousText)
	if err != nil {
		return err
	}
	store := rail.NewStore(*stateDir)
	lock, err := store.Acquire("rollback", 2*time.Second, 10*time.Minute)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, err := store.Load()
	if err != nil {
		return err
	}
	plan, err := rail.BuildRollbackPlan(manifest, state, *environment, previous, a.now())
	if err != nil {
		return err
	}
	if *planOnly {
		if *jsonOutput {
			return printJSON(a.out, plan)
		}
		renderRollback(a.out, plan)
		return nil
	}
	next, err := rail.ApplyRollback(plan, state, *force, a.now())
	if err != nil {
		return err
	}
	if err := store.Save(next); err != nil {
		return err
	}
	if _, err := store.AppendAudit("rollback", *actor, next.Release, map[string]any{
		"environment": *environment, "simulation": true, "safe": plan.Safe, "forced": *force,
	}, a.now()); err != nil {
		return err
	}
	if *jsonOutput {
		return printJSON(a.out, plan)
	}
	fmt.Fprintf(a.out, "simulated rollback in %s: components=%d safe=%t\n", *environment, len(plan.Components), plan.Safe)
	return nil
}

func parseAssignments(input string) (map[string]string, error) {
	result := map[string]string{}
	if strings.TrimSpace(input) == "" {
		return result, nil
	}
	for _, item := range strings.Split(input, ",") {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid assignment %q", item)
		}
		name := strings.TrimSpace(parts[0])
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate assignment for %s", name)
		}
		result[name] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func renderRollback(writer io.Writer, plan rail.RollbackPlan) {
	fmt.Fprintf(writer, "Rollback %s in %s safe=%t\n", plan.Release, plan.Environment, plan.Safe)
	for _, reason := range plan.Reasons {
		fmt.Fprintln(writer, "  WARNING:", reason)
	}
	for _, component := range plan.Components {
		fmt.Fprintf(writer, "  %d. %s: %s -> %s migrations=%d\n", component.Order, component.Name, component.From, component.To, len(component.Migrations))
	}
}

func (a application) diff(args []string) error {
	flags := newFlags("diff", a.err)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 2, "before and after manifest paths"); err != nil {
		return err
	}
	before, _, err := loadManifest(flags.Arg(0))
	if err != nil {
		return err
	}
	after, _, err := loadManifest(flags.Arg(1))
	if err != nil {
		return err
	}
	entries := rail.DiffManifests(before, after)
	if *jsonOutput {
		return printJSON(a.out, entries)
	}
	fmt.Fprint(a.out, rail.RenderDiffText(entries))
	return nil
}

func (a application) report(args []string) error {
	flags := newFlags("report", a.err)
	stateDir := flags.String("state", ".releaserail", "state directory")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := requireArgs(flags, 0, "no positional arguments"); err != nil {
		return err
	}
	store := rail.NewStore(*stateDir)
	state, err := store.Load()
	if err != nil {
		return err
	}
	audit, err := store.VerifyAudit()
	if err != nil {
		return err
	}
	report := rail.BuildReport(state, audit, a.now())
	if *jsonOutput {
		return printJSON(a.out, report)
	}
	fmt.Fprint(a.out, rail.RenderReportText(report))
	if !audit.Valid {
		return fmt.Errorf("audit chain invalid: %s", audit.Error)
	}
	return nil
}
