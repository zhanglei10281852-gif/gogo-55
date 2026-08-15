package rail

import (
	"testing"
	"time"
)

func TestRecoverRecordsAuditEntry(t *testing.T) {
	store := NewStore(t.TempDir())
	now := time.Date(2025, 5, 6, 7, 8, 9, 0, time.UTC)
	state := ReleaseState{Schema: 1, Release: "demo@1.0.0", ManifestHash: "abc", UpdatedAt: now, Environments: map[string]EnvironmentRun{}}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendAudit("apply", "tester", state.Release, map[string]string{"environment": "staging"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	state.Release = "demo@1.1.0"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	before, err := store.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
	recovered, err := store.Recover(snapshots[len(snapshots)-1])
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Release != "demo@1.0.0" {
		t.Fatalf("recovered release = %s", recovered.Release)
	}
	after, err := store.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if !after.Valid {
		t.Fatalf("audit chain became invalid after recovery: %#v", after)
	}
	if after.Records != before.Records+1 {
		t.Fatalf("recovery left no audit record: records went from %d to %d", before.Records, after.Records)
	}
	records, err := store.ReadAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Action != "recover" {
		t.Fatalf("last audit action = %q, want recover", last.Action)
	}
}
