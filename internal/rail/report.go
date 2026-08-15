package rail

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Report struct {
	GeneratedAt   time.Time           `json:"generatedAt"`
	Release       string              `json:"release"`
	ManifestHash  string              `json:"manifestHash"`
	OverallStatus string              `json:"overallStatus"`
	Environments  []EnvironmentReport `json:"environments"`
	Audit         AuditVerification   `json:"audit"`
	Summary       ReportSummary       `json:"summary"`
}

type EnvironmentReport struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Duration   string            `json:"duration,omitempty"`
	Failure    string            `json:"failure,omitempty"`
	Components []ComponentReport `json:"components"`
}

type ComponentReport struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	Wave       int    `json:"wave"`
	Instances  int    `json:"instances"`
	Migrations int    `json:"migrations"`
	Failure    string `json:"failure,omitempty"`
}

type ReportSummary struct {
	Environments  int            `json:"environments"`
	Components    int            `json:"components"`
	Statuses      map[string]int `json:"statuses"`
	Transitions   int            `json:"transitions"`
	Approvals     int            `json:"approvals"`
	HealthSamples int            `json:"healthSamples"`
}

func BuildReport(state ReleaseState, audit AuditVerification, now time.Time) Report {
	report := Report{
		GeneratedAt: now.UTC(), Release: state.Release, ManifestHash: state.ManifestHash,
		OverallStatus: "pending", Audit: audit, Summary: ReportSummary{Statuses: map[string]int{}},
	}
	names := make([]string, 0, len(state.Environments))
	for name := range state.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	overallRank := 0
	for _, name := range names {
		environment := state.Environments[name]
		item := EnvironmentReport{Name: name, Status: environment.Status, Failure: environment.Failure}
		if environment.StartedAt != nil {
			end := now
			if environment.FinishedAt != nil {
				end = *environment.FinishedAt
			}
			item.Duration = end.Sub(*environment.StartedAt).Round(time.Millisecond).String()
		}
		componentNames := make([]string, 0, len(environment.Components))
		for componentName := range environment.Components {
			componentNames = append(componentNames, componentName)
		}
		sort.Strings(componentNames)
		for _, componentName := range componentNames {
			component := environment.Components[componentName]
			item.Components = append(item.Components, ComponentReport{
				Name: componentName, Version: component.Version, Status: component.Status,
				Wave: component.CurrentWave, Instances: len(component.CompletedInstances),
				Migrations: len(component.Migrations), Failure: component.Failure,
			})
			report.Summary.Components++
			report.Summary.Statuses[component.Status]++
		}
		report.Environments = append(report.Environments, item)
		report.Summary.Environments++
		rank := statusRank(environment.Status)
		if rank > overallRank {
			overallRank = rank
			report.OverallStatus = environment.Status
		}
	}
	report.Summary.Transitions = len(state.History)
	report.Summary.Approvals = len(state.Approvals)
	report.Summary.HealthSamples = len(state.Health)
	return report
}
func statusRank(status string) int {
	switch status {
	case "failed":
		return 7
	case "blocked":
		return 6
	case "running":
		return 5
	case "pending":
		return 4
	case "rolled-back":
		return 3
	case "healthy":
		return 2
	case "completed":
		return 1
	default:
		return 0
	}
}

func RenderReportText(report Report) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "ReleaseRail report\n")
	fmt.Fprintf(&builder, "Release: %s\n", report.Release)
	fmt.Fprintf(&builder, "Manifest: %s\n", report.ManifestHash)
	fmt.Fprintf(&builder, "Status: %s\n", report.OverallStatus)
	fmt.Fprintf(&builder, "Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&builder, "Audit: valid=%t records=%d\n", report.Audit.Valid, report.Audit.Records)
	for _, environment := range report.Environments {
		fmt.Fprintf(&builder, "\nEnvironment %s [%s]", environment.Name, environment.Status)
		if environment.Duration != "" {
			fmt.Fprintf(&builder, " duration=%s", environment.Duration)
		}
		builder.WriteByte('\n')
		if environment.Failure != "" {
			fmt.Fprintf(&builder, "  Failure: %s\n", environment.Failure)
		}
		for _, component := range environment.Components {
			fmt.Fprintf(&builder, "  - %s@%s: %s waves=%d instances=%d migrations=%d\n",
				component.Name, component.Version, component.Status, component.Wave, component.Instances, component.Migrations)
			if component.Failure != "" {
				fmt.Fprintf(&builder, "    Failure: %s\n", component.Failure)
			}
		}
	}
	fmt.Fprintf(&builder, "\nSummary: environments=%d components=%d transitions=%d approvals=%d health=%d\n",
		report.Summary.Environments, report.Summary.Components, report.Summary.Transitions,
		report.Summary.Approvals, report.Summary.HealthSamples)
	return builder.String()
}

func DiffManifests(before, after Manifest) []DiffEntry {
	entries := []DiffEntry{}
	if before.Name != after.Name {
		entries = append(entries, DiffEntry{Path: "name", Kind: "changed", Before: before.Name, After: after.Name})
	}
	if before.Version != after.Version {
		entries = append(entries, DiffEntry{Path: "version", Kind: "changed", Before: before.Version, After: after.Version})
	}
	diffMetadata(&entries, "metadata", before.Metadata, after.Metadata)
	beforeEnvironments := map[string]Environment{}
	afterEnvironments := map[string]Environment{}
	for _, environment := range before.Environments {
		beforeEnvironments[environment.Name] = environment
	}
	for _, environment := range after.Environments {
		afterEnvironments[environment.Name] = environment
	}
	diffEnvironments(&entries, beforeEnvironments, afterEnvironments)
	beforeComponents := map[string]Component{}
	afterComponents := map[string]Component{}
	for _, component := range before.Components {
		beforeComponents[component.Name] = component
	}
	for _, component := range after.Components {
		afterComponents[component.Name] = component
	}
	diffComponents(&entries, beforeComponents, afterComponents)
	beforePolicies, _ := json.Marshal(before.Policies)
	afterPolicies, _ := json.Marshal(after.Policies)
	if string(beforePolicies) != string(afterPolicies) {
		entries = append(entries, DiffEntry{Path: "policies", Kind: "changed", Before: string(beforePolicies), After: string(afterPolicies)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Kind < entries[j].Kind
	})
	return entries
}

func diffMetadata(entries *[]DiffEntry, prefix string, before, after map[string]string) {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	for key := range keys {
		left, leftExists := before[key]
		right, rightExists := after[key]
		path := prefix + "." + key
		switch {
		case !leftExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "added", After: right})
		case !rightExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "removed", Before: left})
		case left != right:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "changed", Before: left, After: right})
		}
	}
}

func diffEnvironments(entries *[]DiffEntry, before, after map[string]Environment) {
	names := unionNames(before, after)
	for _, name := range names {
		left, leftExists := before[name]
		right, rightExists := after[name]
		path := "environments." + name
		switch {
		case !leftExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "added", After: encodeCompact(right)})
		case !rightExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "removed", Before: encodeCompact(left)})
		default:
			if left.Rank != right.Rank {
				*entries = append(*entries, DiffEntry{Path: path + ".rank", Kind: "changed", Before: strconvString(left.Rank), After: strconvString(right.Rank)})
			}
			diffMetadata(entries, path+".variables", left.Variables, right.Variables)
			if encodeCompact(left.Gates) != encodeCompact(right.Gates) {
				*entries = append(*entries, DiffEntry{Path: path + ".gates", Kind: "changed", Before: encodeCompact(left.Gates), After: encodeCompact(right.Gates)})
			}
		}
	}
}

func diffComponents(entries *[]DiffEntry, before, after map[string]Component) {
	names := unionNames(before, after)
	for _, name := range names {
		left, leftExists := before[name]
		right, rightExists := after[name]
		path := "components." + name
		switch {
		case !leftExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "added", After: encodeCompact(right)})
		case !rightExists:
			*entries = append(*entries, DiffEntry{Path: path, Kind: "removed", Before: encodeCompact(left)})
		default:
			compareField(entries, path+".version", left.Version, right.Version)
			compareField(entries, path+".artifact", encodeCompact(left.Artifact), encodeCompact(right.Artifact))
			compareField(entries, path+".dependencies", encodeCompact(left.Dependencies), encodeCompact(right.Dependencies))
			compareField(entries, path+".rollout", encodeCompact(left.Rollout), encodeCompact(right.Rollout))
			compareField(entries, path+".health", encodeCompact(left.Health), encodeCompact(right.Health))
			compareField(entries, path+".migrations", encodeCompact(left.Migrations), encodeCompact(right.Migrations))
			compareField(entries, path+".environment", encodeCompact(left.Environment), encodeCompact(right.Environment))
		}
	}
}

func unionNames[T any](before, after map[string]T) []string {
	set := map[string]bool{}
	for name := range before {
		set[name] = true
	}
	for name := range after {
		set[name] = true
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func compareField(entries *[]DiffEntry, path, before, after string) {
	if before != after {
		*entries = append(*entries, DiffEntry{Path: path, Kind: "changed", Before: before, After: after})
	}
}

func encodeCompact(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func strconvString(value int) string {
	return fmt.Sprintf("%d", value)
}

func RenderDiffText(entries []DiffEntry) string {
	if len(entries) == 0 {
		return "No differences.\n"
	}
	var builder strings.Builder
	for _, entry := range entries {
		switch entry.Kind {
		case "added":
			fmt.Fprintf(&builder, "+ %s = %s\n", entry.Path, entry.After)
		case "removed":
			fmt.Fprintf(&builder, "- %s = %s\n", entry.Path, entry.Before)
		default:
			fmt.Fprintf(&builder, "~ %s: %s -> %s\n", entry.Path, entry.Before, entry.After)
		}
	}
	return builder.String()
}
