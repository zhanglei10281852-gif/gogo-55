package rail

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Graph struct {
	nodes    map[string]Component
	outgoing map[string][]string
	incoming map[string][]string
}

func NewGraph(components []Component) (*Graph, error) {
	graph := &Graph{
		nodes:    make(map[string]Component, len(components)),
		outgoing: make(map[string][]string, len(components)),
		incoming: make(map[string][]string, len(components)),
	}
	for _, component := range components {
		if _, exists := graph.nodes[component.Name]; exists {
			return nil, fmt.Errorf("duplicate component %q", component.Name)
		}
		graph.nodes[component.Name] = component
		graph.outgoing[component.Name] = []string{}
		graph.incoming[component.Name] = []string{}
	}
	for _, component := range components {
		seen := map[string]bool{}
		for _, dependency := range component.Dependencies {
			if seen[dependency.Name] {
				return nil, fmt.Errorf("component %q repeats dependency %q", component.Name, dependency.Name)
			}
			seen[dependency.Name] = true
			if _, exists := graph.nodes[dependency.Name]; !exists {
				return nil, fmt.Errorf("component %q depends on unknown component %q", component.Name, dependency.Name)
			}
			graph.outgoing[dependency.Name] = append(graph.outgoing[dependency.Name], component.Name)
			graph.incoming[component.Name] = append(graph.incoming[component.Name], dependency.Name)
		}
	}
	for name := range graph.nodes {
		sort.Strings(graph.outgoing[name])
		sort.Strings(graph.incoming[name])
	}
	if cycle := graph.Cycle(); len(cycle) > 0 {
		return nil, fmt.Errorf("dependency cycle: %s", strings.Join(cycle, " -> "))
	}
	return graph, nil
}

func (g *Graph) Cycle() []string {
	state := map[string]int{}
	stack := []string{}
	position := map[string]int{}
	var cycle []string
	var visit func(string) bool
	visit = func(name string) bool {
		state[name] = 1
		position[name] = len(stack)
		stack = append(stack, name)
		for _, next := range g.outgoing[name] {
			if state[next] == 0 && visit(next) {
				return true
			}
			if state[next] == 1 {
				cycle = append([]string(nil), stack[position[next]:]...)
				cycle = append(cycle, next)
				return true
			}
		}
		stack = stack[:len(stack)-1]
		delete(position, name)
		state[name] = 2
		return false
	}
	names := make([]string, 0, len(g.nodes))
	for name := range g.nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if state[name] == 0 && visit(name) {
			return cycle
		}
	}
	return nil
}
func (g *Graph) Order() ([]string, error) {
	degrees := make(map[string]int, len(g.nodes))
	ready := []string{}
	for name := range g.nodes {
		degrees[name] = len(g.incoming[name])
		if degrees[name] == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	result := make([]string, 0, len(g.nodes))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		result = append(result, name)
		for _, dependent := range g.outgoing[name] {
			degrees[dependent]--
			if degrees[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}
	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("graph contains a cycle")
	}
	return result, nil
}

func (g *Graph) ReverseOrder() ([]string, error) {
	order, err := g.Order()
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(order)-1; left < right; left, right = left+1, right-1 {
		order[left], order[right] = order[right], order[left]
	}
	return order, nil
}

func (g *Graph) Dependencies(name string) []string {
	return append([]string(nil), g.incoming[name]...)
}

func (g *Graph) Dependents(name string) []string {
	return append([]string(nil), g.outgoing[name]...)
}

func (g *Graph) Layers() ([][]string, error) {
	degrees := make(map[string]int, len(g.nodes))
	current := []string{}
	for name := range g.nodes {
		degrees[name] = len(g.incoming[name])
		if degrees[name] == 0 {
			current = append(current, name)
		}
	}
	sort.Strings(current)
	layers := [][]string{}
	visited := 0
	for len(current) > 0 {
		layer := append([]string(nil), current...)
		layers = append(layers, layer)
		visited += len(layer)
		next := []string{}
		for _, name := range layer {
			for _, dependent := range g.outgoing[name] {
				degrees[dependent]--
				if degrees[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		sort.Strings(next)
		current = next
	}
	if visited != len(g.nodes) {
		return nil, fmt.Errorf("graph contains a cycle")
	}
	return layers, nil
}

func (g *Graph) Closure(names []string) ([]string, error) {
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if seen[name] {
			return nil
		}
		if _, exists := g.nodes[name]; !exists {
			return fmt.Errorf("unknown component %q", name)
		}
		seen[name] = true
		for _, dependency := range g.incoming[name] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	order, err := g.Order()
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, name := range order {
		if seen[name] {
			result = append(result, name)
		}
	}
	return result, nil
}

func ValidateManifest(manifest Manifest, baseDir string) ValidationResult {
	result := ValidationResult{Valid: true, Issues: []ValidationIssue{}}
	add := func(path, severity, message string) {
		result.Issues = append(result.Issues, ValidationIssue{Path: path, Severity: severity, Message: message})
		if severity == "error" {
			result.Valid = false
		}
	}
	if strings.TrimSpace(manifest.Name) == "" {
		add("name", "error", "release name is required")
	}
	if _, err := ParseVersion(manifest.Version); err != nil {
		add("version", "error", err.Error())
	}
	if len(manifest.Environments) == 0 {
		add("environments", "error", "at least one environment is required")
	}
	environmentNames := map[string]bool{}
	ranks := map[int]string{}
	allowedKinds := map[string]bool{"approval": true, "condition": true, "artifact": true, "health": true}
	if len(manifest.Policies.AllowedKinds) > 0 {
		allowedKinds = map[string]bool{}
		for _, kind := range manifest.Policies.AllowedKinds {
			allowedKinds[kind] = true
		}
	}
	for index, environment := range manifest.Environments {
		path := fmt.Sprintf("environments[%d]", index)
		if environment.Name == "" {
			add(path+".name", "error", "environment name is required")
		}
		if environmentNames[environment.Name] {
			add(path+".name", "error", "environment name must be unique")
		}
		environmentNames[environment.Name] = true
		if previous, exists := ranks[environment.Rank]; exists {
			add(path+".rank", "warning", fmt.Sprintf("rank is shared with %s; names break ties", previous))
		} else {
			ranks[environment.Rank] = environment.Name
		}
		gateNames := map[string]bool{}
		for gateIndex, gate := range environment.Gates {
			gatePath := fmt.Sprintf("%s.gates[%d]", path, gateIndex)
			if gate.Name == "" {
				add(gatePath+".name", "error", "gate name is required")
			}
			if gateNames[gate.Name] {
				add(gatePath+".name", "error", "gate name must be unique within environment")
			}
			gateNames[gate.Name] = true
			if !allowedKinds[gate.Kind] {
				add(gatePath+".kind", "error", "gate kind is not allowed")
			}
			validateGate(gate, gatePath, add)
		}
	}
	if len(manifest.Components) == 0 {
		add("components", "error", "at least one component is required")
	}
	componentNames := map[string]bool{}
	for index, component := range manifest.Components {
		path := fmt.Sprintf("components[%d]", index)
		if component.Name == "" {
			add(path+".name", "error", "component name is required")
		}
		if componentNames[component.Name] {
			add(path+".name", "error", "component name must be unique")
		}
		componentNames[component.Name] = true
		if _, err := ParseVersion(component.Version); err != nil {
			add(path+".version", "error", err.Error())
		}
		validateArtifact(component.Artifact, path+".artifact", manifest.Policies.RequireArtifact, baseDir, add)
		validateRollout(component.Rollout, path+".rollout", add)
		validateHealth(component.Health, path+".health", add)
		validateMigrations(component.Migrations, path+".migrations", manifest.Policies.RequireRollback, add)
		for environment := range component.Environment {
			if !environmentNames[environment] {
				add(path+".environment."+environment, "error", "tuning references unknown environment")
			}
		}
	}
	graph, err := NewGraph(manifest.Components)
	if err != nil {
		add("components", "error", err.Error())
	} else {
		for index, component := range manifest.Components {
			for dependencyIndex, dependency := range component.Dependencies {
				dependencyComponent := graph.nodes[dependency.Name]
				rangeValue, rangeErr := ParseRange(dependency.Range)
				path := fmt.Sprintf("components[%d].dependencies[%d].range", index, dependencyIndex)
				if rangeErr != nil {
					add(path, "error", rangeErr.Error())
					continue
				}
				version, versionErr := ParseVersion(dependencyComponent.Version)
				if versionErr == nil && !rangeValue.Contains(version) {
					add(path, "error", fmt.Sprintf("%s does not satisfy %s", dependencyComponent.Version, dependency.Range))
				}
			}
		}
	}
	if manifest.Policies.MaxParallel < 0 {
		add("policies.maxParallel", "error", "maxParallel cannot be negative")
	}
	sort.SliceStable(result.Issues, func(i, j int) bool {
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Message < result.Issues[j].Message
	})
	return result
}

func validateGate(gate Gate, path string, add func(string, string, string)) {
	switch gate.Kind {
	case "approval":
		if gate.Required <= 0 {
			add(path+".required", "error", "approval gate requires a positive count")
		}
		unique := map[string]bool{}
		for _, approver := range gate.Approvers {
			if strings.TrimSpace(approver) == "" {
				add(path+".approvers", "error", "approver cannot be empty")
			}
			unique[approver] = true
		}
		if gate.Required > len(unique) {
			add(path+".required", "error", "required approvals exceed eligible approvers")
		}
	case "condition":
		if strings.TrimSpace(gate.Condition) == "" {
			add(path+".condition", "error", "condition gate requires an expression")
		} else if _, err := ParseCondition(gate.Condition); err != nil {
			add(path+".condition", "error", err.Error())
		}
	case "artifact", "health":
		if gate.Required < 0 {
			add(path+".required", "error", "required cannot be negative")
		}
	}
}

func validateArtifact(artifact Artifact, path string, required bool, baseDir string, add func(string, string, string)) {
	if artifact.Path == "" {
		if required {
			add(path+".path", "error", "artifact path is required")
		}
		return
	}
	if filepath.IsAbs(artifact.Path) {
		add(path+".path", "error", "artifact path must be relative to manifest")
	}
	clean := filepath.Clean(artifact.Path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		add(path+".path", "error", "artifact path escapes manifest directory")
	}
	if artifact.Size < 0 {
		add(path+".size", "error", "artifact size cannot be negative")
	}
	if len(artifact.SHA256) != 64 {
		add(path+".sha256", "error", "SHA-256 must contain 64 hexadecimal characters")
	} else if _, err := hex.DecodeString(artifact.SHA256); err != nil {
		add(path+".sha256", "error", "SHA-256 is not hexadecimal")
	}
	if baseDir != "" {
		full := filepath.Join(baseDir, clean)
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			add(path+".path", "error", "artifact path points to a directory")
		}
	}
}

func validateRollout(rollout Rollout, path string, add func(string, string, string)) {
	if rollout.Strategy == "" {
		rollout.Strategy = "waves"
	}
	if rollout.Strategy != "waves" && rollout.Strategy != "all-at-once" {
		add(path+".strategy", "error", "strategy must be waves or all-at-once")
	}
	if rollout.Instances <= 0 {
		add(path+".instances", "error", "instances must be positive")
	}
	if rollout.Strategy == "waves" && rollout.WaveSize <= 0 {
		add(path+".waveSize", "error", "wave size must be positive")
	}
	if rollout.Pause != "" {
		if duration, err := time.ParseDuration(rollout.Pause); err != nil || duration < 0 {
			add(path+".pause", "error", "pause must be a non-negative duration")
		}
	}
}

func validateHealth(criteria []HealthCriterion, path string, add func(string, string, string)) {
	names := map[string]bool{}
	for index, criterion := range criteria {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if criterion.Name == "" || criterion.Metric == "" {
			add(itemPath, "error", "health criterion requires name and metric")
		}
		if names[criterion.Name] {
			add(itemPath+".name", "error", "health criterion name must be unique")
		}
		names[criterion.Name] = true
		if !validHealthOperator(criterion.Operator) {
			add(itemPath+".operator", "error", "unsupported health operator")
		}
		if criterion.Window != "" {
			if duration, err := time.ParseDuration(criterion.Window); err != nil || duration <= 0 {
				add(itemPath+".window", "error", "window must be a positive duration")
			}
		}
	}
}

func validHealthOperator(operator string) bool {
	switch operator {
	case "<", "<=", ">", ">=", "==", "!=":
		return true
	default:
		return false
	}
}

func validateMigrations(migrations []Migration, path string, requireRollback bool, add func(string, string, string)) {
	ids := map[string]bool{}
	for index, migration := range migrations {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if migration.ID == "" {
			add(itemPath+".id", "error", "migration ID is required")
		}
		if ids[migration.ID] {
			add(itemPath+".id", "error", "migration ID must be unique")
		}
		ids[migration.ID] = true
		if migration.Phase != "pre" && migration.Phase != "post" {
			add(itemPath+".phase", "error", "migration phase must be pre or post")
		}
		if strings.TrimSpace(migration.Command) == "" {
			add(itemPath+".command", "error", "migration command is required")
		}
		if requireRollback && (!migration.Reversible || strings.TrimSpace(migration.Rollback) == "") {
			add(itemPath+".rollback", "error", "rollback policy requires reversible migrations and rollback commands")
		}
	}
	for index, migration := range migrations {
		for _, dependency := range migration.DependsOn {
			if !ids[dependency] {
				add(fmt.Sprintf("%s[%d].dependsOn", path, index), "error", "references unknown migration "+dependency)
			}
		}
	}
	if _, err := OrderMigrations(migrations); err != nil {
		add(path, "error", err.Error())
	}
}

func OrderMigrations(migrations []Migration) ([]Migration, error) {
	byID := map[string]Migration{}
	degree := map[string]int{}
	outgoing := map[string][]string{}
	for _, migration := range migrations {
		byID[migration.ID] = migration
		degree[migration.ID] = 0
	}
	for _, migration := range migrations {
		for _, dependency := range migration.DependsOn {
			if _, exists := byID[dependency]; !exists {
				return nil, fmt.Errorf("migration %s depends on unknown migration %s", migration.ID, dependency)
			}
			if byID[dependency].Phase == "post" && migration.Phase == "pre" {
				return nil, fmt.Errorf("pre migration %s cannot depend on post migration %s", migration.ID, dependency)
			}
			degree[migration.ID]++
			outgoing[dependency] = append(outgoing[dependency], migration.ID)
		}
	}
	ready := []string{}
	for id, count := range degree {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		left := byID[ready[i]]
		right := byID[ready[j]]
		if left.Phase != right.Phase {
			return left.Phase == "pre"
		}
		return left.ID < right.ID
	})
	result := []Migration{}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		result = append(result, byID[id])
		for _, next := range outgoing[id] {
			degree[next]--
			if degree[next] == 0 {
				ready = append(ready, next)
			}
		}
		sort.Slice(ready, func(i, j int) bool {
			left := byID[ready[i]]
			right := byID[ready[j]]
			if left.Phase != right.Phase {
				return left.Phase == "pre"
			}
			return left.ID < right.ID
		})
	}
	if len(result) != len(migrations) {
		return nil, fmt.Errorf("migration dependency cycle")
	}
	return result, nil
}

func ManifestHash(manifest Manifest) (string, error) {
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
