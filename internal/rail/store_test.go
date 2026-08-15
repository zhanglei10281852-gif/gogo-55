package rail

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactVerification(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "artifact.bin")
	if err := os.WriteFile(path, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Path: "artifact.bin", Size: 7, SHA256: "a4d451ec23463726f72c43d64c710968f6b602cd653b4de8adee1b556240a829"}
	result := VerifyArtifact("api", artifact, directory)
	if !result.Valid {
		t.Fatalf("verification failed: %s", result.Error)
	}
	artifact.Size = 8
	if result := VerifyArtifact("api", artifact, directory); result.Valid || result.Error == "" {
		t.Fatal("size mismatch was accepted")
	}
	artifact.Path = "../outside.bin"
	if result := VerifyArtifact("api", artifact, directory); result.Valid {
		t.Fatal("escaping path was accepted")
	}
}

func TestStoreSnapshotsRecoveryLockAndAudit(t *testing.T) {
	store := NewStore(t.TempDir())
	lock, err := store.Acquire("test", 0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire("contender", 0, time.Minute); err == nil {
		t.Fatal("second lock unexpectedly succeeded")
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	state := ReleaseState{Schema: 1, Release: "demo@1.0.0", ManifestHash: "abc", UpdatedAt: now, Environments: map[string]EnvironmentRun{}}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	state.Release = "demo@1.1.0"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %v", snapshots)
	}
	recovered, err := store.Recover(snapshots[0])
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Release != "demo@1.0.0" {
		t.Fatalf("recovered release = %s", recovered.Release)
	}
	baseline, err := store.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.AppendAudit("apply", "tester", recovered.Release, map[string]string{"env": "stage"}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendAudit("rollback", "tester", recovered.Release, map[string]string{"env": "stage"}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Previous != first.Hash {
		t.Fatal("audit records are not chained")
	}
	verification, err := store.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Valid || verification.Records != baseline.Records+2 {
		t.Fatalf("audit verification = %#v", verification)
	}
	file, err := os.OpenFile(store.AuditPath(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	verification, err = store.VerifyAudit()
	if err != nil {
		t.Fatal(err)
	}
	if verification.Valid || verification.BrokenLine != baseline.Records+3 {
		t.Fatalf("tampered audit accepted: %#v", verification)
	}
}
