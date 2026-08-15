package rail

import (
	"reflect"
	"testing"
	"time"
)

func testManifest() Manifest {
	return Manifest{
		Name:    "demo",
		Version: "2.0.0",
		Environments: []Environment{
			{Name: "staging", Rank: 1, Variables: map[string]string{"ready": "yes"}, Gates: []Gate{{Name: "ready", Kind: "condition", Condition: "ready=yes"}}},
		},
		Components: []Component{
			{
				Name: "database", Version: "1.4.0",
				Artifact:   Artifact{Path: "db.bin", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1},
				Rollout:    Rollout{Strategy: "waves", Instances: 3, WaveSize: 1, Seed: "fixed"},
				Migrations: []Migration{{ID: "schema", Phase: "pre", Reversible: true, Command: "schema-up", Rollback: "schema-down"}},
			},
			{
				Name: "api", Version: "2.0.0", Dependencies: []Dependency{{Name: "database", Range: "^1.0.0"}},
				Artifact: Artifact{Path: "api.bin", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Size: 1},
				Rollout:  Rollout{Strategy: "waves", Instances: 5, WaveSize: 2, Seed: "fixed"},
			},
		},
		Policies: Policies{RequireArtifact: true, RequireRollback: true},
	}
}

func TestConditionSubstringOperator(t *testing.T) {
	condition, err := ParseCondition("channel~=stable")
	if err != nil {
		t.Fatal(err)
	}
	if condition.Operator != "~=" || condition.Key != "channel" {
		t.Fatalf("condition parsed as %#v", condition)
	}
	if matched, _ := condition.Evaluate(map[string]string{"channel": "preview-stable"}); !matched {
		t.Fatal("substring condition did not match")
	}
}

func TestGraphOrderAndCycle(t *testing.T) {
	manifest := testManifest()
	graph, err := NewGraph(manifest.Components)
	if err != nil {
		t.Fatal(err)
	}
	order, err := graph.Order()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"database", "api"}) {
		t.Fatalf("order = %v", order)
	}
	manifest.Components[0].Dependencies = []Dependency{{Name: "api", Range: "*"}}
	if _, err := NewGraph(manifest.Components); err == nil {
		t.Fatal("dependency cycle was accepted")
	}
}

func TestDeterministicWavesCoverEveryInstance(t *testing.T) {
	manifest := testManifest()
	component := manifest.Components[1]
	environment := manifest.Environments[0]
	first := DeterministicWaves(component, environment, "hash")
	second := DeterministicWaves(component, environment, "hash")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("waves are not deterministic: %v versus %v", first, second)
	}
	if len(first) != 3 {
		t.Fatalf("wave count = %d", len(first))
	}
	seen := map[int]bool{}
	for _, wave := range first {
		if len(wave) > 2 {
			t.Fatalf("wave too large: %v", wave)
		}
		for _, instance := range wave {
			if seen[instance] {
				t.Fatalf("duplicate instance %d", instance)
			}
			seen[instance] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("covered %d instances", len(seen))
	}
}

func TestPlanApplyAndRollback(t *testing.T) {
	manifest := testManifest()
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	hash, err := ManifestHash(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := InitializeState(manifest, hash, now)
	artifacts := []ArtifactResult{{Component: "database", Valid: true}, {Component: "api", Valid: true}}
	plan, err := BuildPlan(manifest, state, artifacts, now)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Environments[0].Blocked() {
		t.Fatal("condition gate unexpectedly blocked")
	}
	applied, err := ApplySimulation(plan, state, "staging", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if applied.Environments["staging"].Status != "completed" {
		t.Fatalf("status = %s", applied.Environments["staging"].Status)
	}
	rollback, err := BuildRollbackPlan(manifest, applied, "staging", map[string]string{"api": "1.9.0", "database": "1.3.0"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !rollback.Safe || rollback.Components[0].Name != "api" {
		t.Fatalf("rollback = %#v", rollback)
	}
	environment := applied.Environments["staging"]
	environment.Status = "failed"
	applied.Environments["staging"] = environment
	rolledBack, err := ApplyRollback(rollback, applied, false, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Environments["staging"].Components["api"].Version != "1.9.0" {
		t.Fatal("rollback target not applied")
	}
	if transition := rolledBack.History[len(rolledBack.History)-1]; transition.From != "failed" {
		t.Fatalf("rollback environment source status = %s", transition.From)
	}
}
