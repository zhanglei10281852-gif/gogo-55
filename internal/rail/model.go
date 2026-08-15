package rail

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Manifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Environments []Environment     `json:"environments"`
	Components   []Component       `json:"components"`
	Policies     Policies          `json:"policies"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Environment struct {
	Name      string            `json:"name"`
	Rank      int               `json:"rank"`
	Variables map[string]string `json:"variables,omitempty"`
	Gates     []Gate            `json:"gates,omitempty"`
}

type Gate struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Required  int      `json:"required,omitempty"`
	Approvers []string `json:"approvers,omitempty"`
	Condition string   `json:"condition,omitempty"`
}

type Component struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies []Dependency      `json:"dependencies,omitempty"`
	Artifact     Artifact          `json:"artifact"`
	Rollout      Rollout           `json:"rollout"`
	Health       []HealthCriterion `json:"health,omitempty"`
	Migrations   []Migration       `json:"migrations,omitempty"`
	Environment  map[string]Tuning `json:"environment,omitempty"`
}
type Dependency struct {
	Name  string `json:"name"`
	Range string `json:"range"`
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Rollout struct {
	Strategy  string `json:"strategy"`
	WaveSize  int    `json:"waveSize,omitempty"`
	Seed      string `json:"seed,omitempty"`
	Instances int    `json:"instances,omitempty"`
	Pause     string `json:"pause,omitempty"`
}

type HealthCriterion struct {
	Name      string  `json:"name"`
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Window    string  `json:"window,omitempty"`
	Required  bool    `json:"required,omitempty"`
}

type Migration struct {
	ID         string   `json:"id"`
	Phase      string   `json:"phase"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	Reversible bool     `json:"reversible"`
	Command    string   `json:"command"`
	Rollback   string   `json:"rollback,omitempty"`
}

type Tuning struct {
	Instances int               `json:"instances,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Disabled  bool              `json:"disabled,omitempty"`
}

type Policies struct {
	RequireArtifact bool     `json:"requireArtifact,omitempty"`
	RequireRollback bool     `json:"requireRollback,omitempty"`
	MaxParallel     int      `json:"maxParallel,omitempty"`
	AllowedKinds    []string `json:"allowedGateKinds,omitempty"`
}

type Approval struct {
	Environment string    `json:"environment"`
	Gate        string    `json:"gate"`
	Approver    string    `json:"approver"`
	GrantedAt   time.Time `json:"grantedAt"`
}

type HealthSample struct {
	Environment string    `json:"environment"`
	Component   string    `json:"component"`
	Metric      string    `json:"metric"`
	Value       float64   `json:"value"`
	ObservedAt  time.Time `json:"observedAt"`
}

type ReleaseState struct {
	Schema       int                       `json:"schema"`
	ManifestHash string                    `json:"manifestHash"`
	Release      string                    `json:"release"`
	UpdatedAt    time.Time                 `json:"updatedAt"`
	Environments map[string]EnvironmentRun `json:"environments"`
	Approvals    []Approval                `json:"approvals,omitempty"`
	Health       []HealthSample            `json:"health,omitempty"`
	History      []Transition              `json:"history,omitempty"`
}

type EnvironmentRun struct {
	Name       string                  `json:"name"`
	Status     string                  `json:"status"`
	StartedAt  *time.Time              `json:"startedAt,omitempty"`
	FinishedAt *time.Time              `json:"finishedAt,omitempty"`
	Components map[string]ComponentRun `json:"components"`
	Failure    string                  `json:"failure,omitempty"`
}

type ComponentRun struct {
	Name               string    `json:"name"`
	Version            string    `json:"version"`
	Status             string    `json:"status"`
	CurrentWave        int       `json:"currentWave"`
	CompletedInstances []int     `json:"completedInstances,omitempty"`
	Migrations         []string  `json:"migrations,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Failure            string    `json:"failure,omitempty"`
}

type Transition struct {
	At          time.Time `json:"at"`
	Environment string    `json:"environment"`
	Component   string    `json:"component,omitempty"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Reason      string    `json:"reason,omitempty"`
}

type Plan struct {
	Release      string            `json:"release"`
	ManifestHash string            `json:"manifestHash"`
	CreatedAt    time.Time         `json:"createdAt"`
	Environments []EnvironmentPlan `json:"environments"`
	Warnings     []string          `json:"warnings,omitempty"`
}

type EnvironmentPlan struct {
	Name       string          `json:"name"`
	Rank       int             `json:"rank"`
	Gates      []GateDecision  `json:"gates,omitempty"`
	Components []ComponentPlan `json:"components"`
}

type GateDecision struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Satisfied   bool     `json:"satisfied"`
	GrantedBy   []string `json:"grantedBy,omitempty"`
	Required    int      `json:"required,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
}

type ComponentPlan struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Order      int               `json:"order"`
	Waves      [][]int           `json:"waves"`
	Migrations []Migration       `json:"migrations,omitempty"`
	Health     []HealthCriterion `json:"health,omitempty"`
	Artifact   Artifact          `json:"artifact"`
}

type RollbackPlan struct {
	Release     string              `json:"release"`
	Environment string              `json:"environment"`
	CreatedAt   time.Time           `json:"createdAt"`
	Safe        bool                `json:"safe"`
	Reasons     []string            `json:"reasons,omitempty"`
	Components  []RollbackComponent `json:"components"`
}

type RollbackComponent struct {
	Name       string      `json:"name"`
	From       string      `json:"from"`
	To         string      `json:"to"`
	Order      int         `json:"order"`
	Migrations []Migration `json:"migrations,omitempty"`
}

type ValidationIssue struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}

type ArtifactResult struct {
	Component string `json:"component"`
	Path      string `json:"path"`
	Valid     bool   `json:"valid"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Error     string `json:"error,omitempty"`
}

type DiffEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func (m Manifest) Environment(name string) (Environment, bool) {
	for _, env := range m.Environments {
		if env.Name == name {
			return env, true
		}
	}
	return Environment{}, false
}

func (m Manifest) Component(name string) (Component, bool) {
	for _, component := range m.Components {
		if component.Name == name {
			return component, true
		}
	}
	return Component{}, false
}

func (m Manifest) ReleaseID() string {
	return m.Name + "@" + m.Version
}

func (m Manifest) SortedEnvironments() []Environment {
	result := append([]Environment(nil), m.Environments...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Rank != result[j].Rank {
			return result[i].Rank < result[j].Rank
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (m Manifest) Names() []string {
	result := make([]string, 0, len(m.Components))
	for _, component := range m.Components {
		result = append(result, component.Name)
	}
	sort.Strings(result)
	return result
}

func (r *ReleaseState) ensure() {
	if r.Schema == 0 {
		r.Schema = 1
	}
	if r.Environments == nil {
		r.Environments = map[string]EnvironmentRun{}
	}
}

func (r *ReleaseState) Environment(name string) (EnvironmentRun, bool) {
	r.ensure()
	env, ok := r.Environments[name]
	return env, ok
}

func (r *ReleaseState) SetEnvironment(env EnvironmentRun) {
	r.ensure()
	r.Environments[env.Name] = env
}

func (r *ReleaseState) AddTransition(t Transition) {
	r.ensure()
	r.History = append(r.History, t)
	r.UpdatedAt = t.At
}

func (r ReleaseState) Clone() ReleaseState {
	clone := r
	clone.Approvals = append([]Approval(nil), r.Approvals...)
	clone.Health = append([]HealthSample(nil), r.Health...)
	clone.History = append([]Transition(nil), r.History...)
	clone.Environments = make(map[string]EnvironmentRun, len(r.Environments))
	for name, env := range r.Environments {
		copyEnv := env
		copyEnv.Components = make(map[string]ComponentRun, len(env.Components))
		for componentName, component := range env.Components {
			copyComponent := component
			copyComponent.CompletedInstances = append([]int(nil), component.CompletedInstances...)
			copyComponent.Migrations = append([]string(nil), component.Migrations...)
			copyEnv.Components[componentName] = copyComponent
		}
		clone.Environments[name] = copyEnv
	}
	return clone
}

func (r ReleaseState) ApprovalSet(environment, gate string) map[string]time.Time {
	result := map[string]time.Time{}
	for _, approval := range r.Approvals {
		if approval.Environment == environment && approval.Gate == gate {
			if current, ok := result[approval.Approver]; !ok || current.Before(approval.GrantedAt) {
				result[approval.Approver] = approval.GrantedAt
			}
		}
	}
	return result
}

func (r ReleaseState) LatestHealth(environment, component, metric string) (HealthSample, bool) {
	var result HealthSample
	found := false
	for _, sample := range r.Health {
		if sample.Environment != environment || sample.Component != component || sample.Metric != metric {
			continue
		}
		if !found || result.ObservedAt.Before(sample.ObservedAt) {
			result = sample
			found = true
		}
	}
	return result, found
}

func normalizeStatus(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "pending", "blocked", "running", "healthy", "failed", "rolled-back", "completed":
		return value, nil
	default:
		return "", fmt.Errorf("unknown status %q", value)
	}
}

func validTransition(from, to string) bool {
	allowed := map[string]map[string]bool{
		"pending":     {"blocked": true, "running": true, "rolled-back": true},
		"blocked":     {"pending": true, "running": true, "rolled-back": true},
		"running":     {"healthy": true, "failed": true, "completed": true, "rolled-back": true},
		"healthy":     {"running": true, "completed": true, "failed": true, "rolled-back": true},
		"failed":      {"running": true, "rolled-back": true},
		"completed":   {"rolled-back": true},
		"rolled-back": {"pending": true},
	}
	return allowed[from][to]
}

func (r *ReleaseState) TransitionEnvironment(name, to, reason string, now time.Time) error {
	to, err := normalizeStatus(to)
	if err != nil {
		return err
	}
	env, ok := r.Environment(name)
	if !ok {
		return fmt.Errorf("environment %q not initialized", name)
	}
	from := env.Status
	if from == "" {
		from = "pending"
	}
	if from != to && !validTransition(from, to) {
		return fmt.Errorf("invalid environment transition %s -> %s", from, to)
	}
	if to == "running" && env.StartedAt == nil {
		started := now
		env.StartedAt = &started
	}
	if to == "completed" || to == "rolled-back" {
		finished := now
		env.FinishedAt = &finished
	}
	env.Status = to
	if to == "failed" {
		env.Failure = reason
	}
	r.SetEnvironment(env)
	r.AddTransition(Transition{At: now, Environment: name, From: from, To: to, Reason: reason})
	return nil
}

func (r *ReleaseState) TransitionComponent(environment, name, to, reason string, now time.Time) error {
	to, err := normalizeStatus(to)
	if err != nil {
		return err
	}
	env, ok := r.Environment(environment)
	if !ok {
		return fmt.Errorf("environment %q not initialized", environment)
	}
	component, ok := env.Components[name]
	if !ok {
		return fmt.Errorf("component %q not initialized", name)
	}
	from := component.Status
	if from == "" {
		from = "pending"
	}
	if from != to && !validTransition(from, to) {
		return fmt.Errorf("invalid component transition %s -> %s", from, to)
	}
	component.Status = to
	component.UpdatedAt = now
	if to == "failed" {
		component.Failure = reason
	}
	env.Components[name] = component
	r.SetEnvironment(env)
	r.AddTransition(Transition{At: now, Environment: environment, Component: name, From: from, To: to, Reason: reason})
	return nil
}

func (p Plan) Environment(name string) (EnvironmentPlan, bool) {
	for _, environment := range p.Environments {
		if environment.Name == name {
			return environment, true
		}
	}
	return EnvironmentPlan{}, false
}

func (p EnvironmentPlan) Blocked() bool {
	for _, gate := range p.Gates {
		if !gate.Satisfied {
			return true
		}
	}
	return false
}

func (p EnvironmentPlan) Component(name string) (ComponentPlan, bool) {
	for _, component := range p.Components {
		if component.Name == name {
			return component, true
		}
	}
	return ComponentPlan{}, false
}

func (v ValidationResult) Error() error {
	if v.Valid {
		return nil
	}
	parts := make([]string, 0, len(v.Issues))
	for _, issue := range v.Issues {
		if issue.Severity == "error" {
			parts = append(parts, issue.Path+": "+issue.Message)
		}
	}
	if len(parts) == 0 {
		return errors.New("manifest is invalid")
	}
	return errors.New(strings.Join(parts, "; "))
}
