package rail

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Condition struct {
	Key      string
	Operator string
	Value    string
}

func ParseCondition(input string) (Condition, error) {
	input = strings.TrimSpace(input)
	for _, operator := range []string{"!=", "==", "~=", "="} {
		if index := strings.Index(input, operator); index >= 0 {
			key := strings.TrimSpace(input[:index])
			value := strings.TrimSpace(input[index+len(operator):])
			if key == "" || value == "" {
				return Condition{}, fmt.Errorf("condition requires non-empty key and value")
			}
			if strings.ContainsAny(key, " \t") {
				return Condition{}, fmt.Errorf("condition key cannot contain whitespace")
			}
			return Condition{Key: key, Operator: operator, Value: unquoteCondition(value)}, nil
		}
	}
	return Condition{}, fmt.Errorf("condition must use =, ==, !=, or ~=")
}

func unquoteCondition(value string) string {
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			if decoded, err := strconv.Unquote(value); err == nil {
				return decoded
			}
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func (c Condition) Evaluate(values map[string]string) (bool, string) {
	actual, exists := values[c.Key]
	if !exists {
		return false, fmt.Sprintf("variable %s is unset", c.Key)
	}
	var matched bool
	switch c.Operator {
	case "=", "==":
		matched = actual == c.Value
	case "!=":
		matched = actual != c.Value
	case "~=":
		matched = strings.Contains(actual, c.Value)
	}
	if matched {
		return true, fmt.Sprintf("%s %s %s", c.Key, c.Operator, c.Value)
	}
	return false, fmt.Sprintf("%s is %q, expected %s %q", c.Key, actual, c.Operator, c.Value)
}
func EvaluateGate(gate Gate, environment Environment, state ReleaseState, artifacts map[string]ArtifactResult) GateDecision {
	decision := GateDecision{Name: gate.Name, Kind: gate.Kind, Required: gate.Required}
	switch gate.Kind {
	case "approval":
		granted := state.ApprovalSet(environment.Name, gate.Name)
		eligible := map[string]bool{}
		for _, approver := range gate.Approvers {
			eligible[approver] = true
		}
		for approver := range granted {
			if eligible[approver] {
				decision.GrantedBy = append(decision.GrantedBy, approver)
			}
		}
		sort.Strings(decision.GrantedBy)
		decision.Satisfied = len(decision.GrantedBy) >= gate.Required
		decision.Explanation = fmt.Sprintf("%d of %d required approvals", len(decision.GrantedBy), gate.Required)
	case "condition":
		condition, err := ParseCondition(gate.Condition)
		if err != nil {
			decision.Explanation = err.Error()
			return decision
		}
		decision.Satisfied, decision.Explanation = condition.Evaluate(environment.Variables)
	case "artifact":
		decision.Satisfied = len(artifacts) > 0
		for name, result := range artifacts {
			if !result.Valid {
				decision.Satisfied = false
				decision.Explanation = "artifact failed verification for " + name
				break
			}
		}
		if decision.Satisfied {
			decision.Explanation = "all artifacts verified"
		}
	case "health":
		decision.Satisfied = true
		for _, sample := range state.Health {
			if sample.Environment == environment.Name {
				continue
			}
		}
		decision.Explanation = "health is evaluated per rollout wave"
	default:
		decision.Explanation = "unsupported gate kind"
	}
	return decision
}

func DeterministicWaves(component Component, environment Environment, manifestHash string) [][]int {
	instances := component.Rollout.Instances
	if tuning, exists := component.Environment[environment.Name]; exists && tuning.Instances > 0 {
		instances = tuning.Instances
	}
	if instances <= 0 {
		return [][]int{}
	}
	waveSize := component.Rollout.WaveSize
	if component.Rollout.Strategy == "all-at-once" || waveSize <= 0 {
		waveSize = instances
	}
	indices := make([]int, instances)
	for index := range indices {
		indices[index] = index + 1
	}
	seed := component.Rollout.Seed
	if seed == "" {
		seed = manifestHash
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left := rolloutScore(seed, environment.Name, component.Name, indices[i])
		right := rolloutScore(seed, environment.Name, component.Name, indices[j])
		if left != right {
			return left < right
		}
		return indices[i] < indices[j]
	})
	waves := make([][]int, 0, (instances+waveSize-1)/waveSize)
	for start := 0; start < instances; start += waveSize {
		end := start + waveSize
		if end > instances {
			end = instances
		}
		waves = append(waves, append([]int(nil), indices[start:end]...))
	}
	return waves
}

func rolloutScore(seed, environment, component string, instance int) uint64 {
	input := fmt.Sprintf("%s\x00%s\x00%s\x00%d", seed, environment, component, instance)
	digest := sha256.Sum256([]byte(input))
	return binary.BigEndian.Uint64(digest[:8])
}

func BuildPlan(manifest Manifest, state ReleaseState, artifactResults []ArtifactResult, now time.Time) (Plan, error) {
	validation := ValidateManifest(manifest, "")
	if err := validation.Error(); err != nil {
		return Plan{}, err
	}
	graph, err := NewGraph(manifest.Components)
	if err != nil {
		return Plan{}, err
	}
	order, err := graph.Order()
	if err != nil {
		return Plan{}, err
	}
	hash, err := ManifestHash(manifest)
	if err != nil {
		return Plan{}, err
	}
	artifacts := map[string]ArtifactResult{}
	for _, result := range artifactResults {
		artifacts[result.Component] = result
	}
	plan := Plan{Release: manifest.ReleaseID(), ManifestHash: hash, CreatedAt: now.UTC()}
	for _, environment := range manifest.SortedEnvironments() {
		environmentPlan := EnvironmentPlan{Name: environment.Name, Rank: environment.Rank}
		for _, gate := range environment.Gates {
			environmentPlan.Gates = append(environmentPlan.Gates, EvaluateGate(gate, environment, state, artifacts))
		}
		for index, name := range order {
			component, _ := manifest.Component(name)
			if tuning, exists := component.Environment[environment.Name]; exists && tuning.Disabled {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is disabled in %s", name, environment.Name))
				continue
			}
			migrations, migrationErr := OrderMigrations(component.Migrations)
			if migrationErr != nil {
				return Plan{}, migrationErr
			}
			environmentPlan.Components = append(environmentPlan.Components, ComponentPlan{
				Name: component.Name, Version: component.Version, Order: index + 1,
				Waves: DeterministicWaves(component, environment, hash), Migrations: migrations,
				Health: append([]HealthCriterion(nil), component.Health...), Artifact: component.Artifact,
			})
		}
		plan.Environments = append(plan.Environments, environmentPlan)
	}
	sort.Strings(plan.Warnings)
	return plan, nil
}

func InitializeState(manifest Manifest, hash string, now time.Time) ReleaseState {
	state := ReleaseState{
		Schema: 1, ManifestHash: hash, Release: manifest.ReleaseID(), UpdatedAt: now.UTC(),
		Environments: map[string]EnvironmentRun{},
	}
	for _, environment := range manifest.Environments {
		run := EnvironmentRun{Name: environment.Name, Status: "pending", Components: map[string]ComponentRun{}}
		for _, component := range manifest.Components {
			if tuning, exists := component.Environment[environment.Name]; exists && tuning.Disabled {
				continue
			}
			run.Components[component.Name] = ComponentRun{
				Name: component.Name, Version: component.Version, Status: "pending", UpdatedAt: now.UTC(),
			}
		}
		state.Environments[environment.Name] = run
	}
	return state
}

func CheckHealth(criterion HealthCriterion, value float64) bool {
	switch criterion.Operator {
	case "<":
		return value < criterion.Threshold
	case "<=":
		return value <= criterion.Threshold
	case ">":
		return value > criterion.Threshold
	case ">=":
		return value >= criterion.Threshold
	case "==":
		return value == criterion.Threshold
	case "!=":
		return value != criterion.Threshold
	default:
		return false
	}
}

func EvaluateHealth(environment string, plan ComponentPlan, state ReleaseState) (bool, []string) {
	failures := []string{}
	for _, criterion := range plan.Health {
		sample, found := state.LatestHealth(environment, plan.Name, criterion.Metric)
		if !found {
			if criterion.Required {
				failures = append(failures, criterion.Name+": no sample")
			}
			continue
		}
		if !CheckHealth(criterion, sample.Value) {
			failures = append(failures, fmt.Sprintf("%s: %g %s %g failed", criterion.Name, sample.Value, criterion.Operator, criterion.Threshold))
		}
	}
	return len(failures) == 0, failures
}

func ApplySimulation(plan Plan, current ReleaseState, environmentName string, now time.Time) (ReleaseState, error) {
	environmentPlan, exists := plan.Environment(environmentName)
	if !exists {
		return current, fmt.Errorf("environment %q is not in plan", environmentName)
	}
	if environmentPlan.Blocked() {
		return current, fmt.Errorf("environment %q is blocked by gates", environmentName)
	}
	state := current.Clone()
	if state.Release == "" {
		return state, fmt.Errorf("state has not been initialized")
	}
	if state.ManifestHash != plan.ManifestHash {
		return state, fmt.Errorf("plan manifest hash does not match state")
	}
	if err := state.TransitionEnvironment(environmentName, "running", "offline apply simulation", now); err != nil {
		return current, err
	}
	for _, componentPlan := range environmentPlan.Components {
		if err := state.TransitionComponent(environmentName, componentPlan.Name, "running", "simulation started", now); err != nil {
			return current, err
		}
		environment := state.Environments[environmentName]
		component := environment.Components[componentPlan.Name]
		for _, migration := range componentPlan.Migrations {
			component.Migrations = append(component.Migrations, migration.ID)
		}
		for waveIndex, wave := range componentPlan.Waves {
			component.CurrentWave = waveIndex + 1
			component.CompletedInstances = append(component.CompletedInstances, wave...)
		}
		component.UpdatedAt = now.UTC()
		environment.Components[componentPlan.Name] = component
		state.Environments[environmentName] = environment
		healthy, failures := EvaluateHealth(environmentName, componentPlan, state)
		if !healthy {
			reason := strings.Join(failures, "; ")
			_ = state.TransitionComponent(environmentName, componentPlan.Name, "failed", reason, now)
			_ = state.TransitionEnvironment(environmentName, "failed", reason, now)
			return state, fmt.Errorf("health criteria failed for %s: %s", componentPlan.Name, reason)
		}
		if err := state.TransitionComponent(environmentName, componentPlan.Name, "completed", "simulation completed", now); err != nil {
			return current, err
		}
	}
	if err := state.TransitionEnvironment(environmentName, "completed", "offline apply simulation completed", now); err != nil {
		return current, err
	}
	return state, nil
}

func BuildRollbackPlan(manifest Manifest, state ReleaseState, environment string, previous map[string]string, now time.Time) (RollbackPlan, error) {
	graph, err := NewGraph(manifest.Components)
	if err != nil {
		return RollbackPlan{}, err
	}
	order, err := graph.ReverseOrder()
	if err != nil {
		return RollbackPlan{}, err
	}
	run, exists := state.Environment(environment)
	if !exists {
		return RollbackPlan{}, fmt.Errorf("environment %q has no state", environment)
	}
	plan := RollbackPlan{Release: state.Release, Environment: environment, CreatedAt: now.UTC(), Safe: true}
	for index, name := range order {
		componentRun, deployed := run.Components[name]
		if !deployed || (componentRun.Status != "completed" && componentRun.Status != "failed" && componentRun.Status != "running") {
			continue
		}
		component, _ := manifest.Component(name)
		target, known := previous[name]
		if !known || target == "" {
			plan.Safe = false
			plan.Reasons = append(plan.Reasons, "no previous version recorded for "+name)
			target = "unknown"
		} else if _, parseErr := ParseVersion(target); parseErr != nil {
			plan.Safe = false
			plan.Reasons = append(plan.Reasons, "invalid previous version for "+name)
		}
		reverseMigrations := []Migration{}
		ordered, migrationErr := OrderMigrations(component.Migrations)
		if migrationErr != nil {
			return RollbackPlan{}, migrationErr
		}
		for migrationIndex := len(ordered) - 1; migrationIndex >= 0; migrationIndex-- {
			migration := ordered[migrationIndex]
			if !migration.Reversible || migration.Rollback == "" {
				plan.Safe = false
				plan.Reasons = append(plan.Reasons, fmt.Sprintf("migration %s for %s is irreversible", migration.ID, name))
				continue
			}
			reverseMigrations = append(reverseMigrations, migration)
		}
		plan.Components = append(plan.Components, RollbackComponent{
			Name: name, From: componentRun.Version, To: target, Order: index + 1, Migrations: reverseMigrations,
		})
	}
	sort.Strings(plan.Reasons)
	return plan, nil
}

func ApplyRollback(plan RollbackPlan, state ReleaseState, force bool, now time.Time) (ReleaseState, error) {
	if !plan.Safe && !force {
		return state, fmt.Errorf("rollback is unsafe: %s", strings.Join(plan.Reasons, "; "))
	}
	result := state.Clone()
	environment, exists := result.Environment(plan.Environment)
	if !exists {
		return state, fmt.Errorf("environment %q has no state", plan.Environment)
	}
	environmentFrom := environment.Status
	for _, item := range plan.Components {
		component, exists := environment.Components[item.Name]
		if !exists {
			continue
		}
		componentFrom := component.Status
		component.Version = item.To
		component.Status = "rolled-back"
		component.CurrentWave = 0
		component.CompletedInstances = nil
		component.UpdatedAt = now.UTC()
		for _, migration := range item.Migrations {
			component.Migrations = removeString(component.Migrations, migration.ID)
		}
		environment.Components[item.Name] = component
		result.AddTransition(Transition{At: now.UTC(), Environment: plan.Environment, Component: item.Name, From: componentFrom, To: "rolled-back", Reason: "offline rollback simulation"})
	}
	environment.Status = "rolled-back"
	finished := now.UTC()
	environment.FinishedAt = &finished
	result.SetEnvironment(environment)
	result.AddTransition(Transition{At: now.UTC(), Environment: plan.Environment, From: environmentFrom, To: "rolled-back", Reason: "offline rollback simulation"})
	return result, nil
}

func removeString(values []string, target string) []string {
	result := values[:0]
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}
