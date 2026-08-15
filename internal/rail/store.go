package rail

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Store struct {
	Root string
}

type Lock struct {
	path string
	file *os.File
}

type lockRecord struct {
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"createdAt"`
	Purpose   string    `json:"purpose"`
}

type AuditRecord struct {
	Sequence int64           `json:"sequence"`
	Time     time.Time       `json:"time"`
	Action   string          `json:"action"`
	Actor    string          `json:"actor"`
	Release  string          `json:"release,omitempty"`
	Details  json.RawMessage `json:"details,omitempty"`
	Previous string          `json:"previous"`
	Hash     string          `json:"hash"`
}

type AuditVerification struct {
	Valid      bool   `json:"valid"`
	Records    int64  `json:"records"`
	LastHash   string `json:"lastHash,omitempty"`
	BrokenLine int64  `json:"brokenLine,omitempty"`
	Error      string `json:"error,omitempty"`
}

func NewStore(root string) Store {
	return Store{Root: root}
}

func (s Store) ensure() error {
	if s.Root == "" {
		return fmt.Errorf("state directory is empty")
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return nil
}

func (s Store) StatePath() string {
	return filepath.Join(s.Root, "state.json")
}

func (s Store) AuditPath() string {
	return filepath.Join(s.Root, "audit.jsonl")
}

func (s Store) SnapshotDir() string {
	return filepath.Join(s.Root, "snapshots")
}

func (s Store) Acquire(purpose string, timeout, staleAfter time.Duration) (*Lock, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, "state.lock")
	deadline := time.Now().Add(timeout)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			record := lockRecord{PID: os.Getpid(), CreatedAt: time.Now().UTC(), Purpose: purpose}
			if encodeErr := json.NewEncoder(file).Encode(record); encodeErr != nil {
				file.Close()
				os.Remove(path)
				return nil, fmt.Errorf("write lock: %w", encodeErr)
			}
			if syncErr := file.Sync(); syncErr != nil {
				file.Close()
				os.Remove(path)
				return nil, fmt.Errorf("sync lock: %w", syncErr)
			}
			return &Lock{path: path, file: file}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		if staleAfter > 0 {
			info, statErr := os.Stat(path)
			if statErr == nil && time.Since(info.ModTime()) > staleAfter {
				staleName := path + ".stale-" + strconv.FormatInt(time.Now().UnixNano(), 10)
				if renameErr := os.Rename(path, staleName); renameErr == nil {
					_ = os.Remove(staleName)
					continue
				}
			}
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return nil, fmt.Errorf("state is locked")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	var closeErr error
	if l.file != nil {
		closeErr = l.file.Close()
		l.file = nil
	}
	removeErr := os.Remove(l.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return closeErr
}

func (s Store) Load() (ReleaseState, error) {
	data, err := os.ReadFile(s.StatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReleaseState{}, os.ErrNotExist
		}
		return ReleaseState{}, fmt.Errorf("read state: %w", err)
	}
	var state ReleaseState
	if err := decodeStrictJSON(data, &state); err != nil {
		return ReleaseState{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Schema != 1 {
		return ReleaseState{}, fmt.Errorf("unsupported state schema %d", state.Schema)
	}
	state.ensure()
	return state, nil
}

func (s Store) Save(state ReleaseState) error {
	if err := s.ensure(); err != nil {
		return err
	}
	state.ensure()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	path := s.StatePath()
	if previous, readErr := os.ReadFile(path); readErr == nil {
		if snapshotErr := s.writeSnapshot(previous); snapshotErr != nil {
			return snapshotErr
		}
	}
	return writeAtomic(path, data, 0o600)
}

func (s Store) writeSnapshot(data []byte) error {
	if err := os.MkdirAll(s.SnapshotDir(), 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	digest := sha256.Sum256(data)
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(digest[:6]) + ".json"
	path := filepath.Join(s.SnapshotDir(), name)
	if err := writeAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".releaserail-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		if secondErr := os.Rename(temporaryName, path); secondErr != nil {
			return secondErr
		}
	}
	return nil
}

func (s Store) Snapshots() ([]string, error) {
	entries, err := os.ReadDir(s.SnapshotDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			result = append(result, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	return result, nil
}

func (s Store) Recover(snapshot string) (ReleaseState, error) {
	if snapshot == "" {
		snapshots, err := s.Snapshots()
		if err != nil {
			return ReleaseState{}, err
		}
		if len(snapshots) == 0 {
			return ReleaseState{}, fmt.Errorf("no snapshots available")
		}
		snapshot = snapshots[0]
	}
	if filepath.Base(snapshot) != snapshot {
		return ReleaseState{}, fmt.Errorf("invalid snapshot name")
	}
	data, err := os.ReadFile(filepath.Join(s.SnapshotDir(), snapshot))
	if err != nil {
		return ReleaseState{}, fmt.Errorf("read snapshot: %w", err)
	}
	var state ReleaseState
	if err := decodeStrictJSON(data, &state); err != nil {
		return ReleaseState{}, fmt.Errorf("invalid snapshot: %w", err)
	}
	if state.Schema != 1 {
		return ReleaseState{}, fmt.Errorf("unsupported snapshot schema")
	}
	if err := s.Save(state); err != nil {
		return ReleaseState{}, err
	}
	return state, nil
}

func (s Store) AppendAudit(action, actor, release string, details any, now time.Time) (AuditRecord, error) {
	if err := s.ensure(); err != nil {
		return AuditRecord{}, err
	}
	verification, err := s.VerifyAudit()
	if err != nil {
		return AuditRecord{}, err
	}
	if !verification.Valid {
		return AuditRecord{}, fmt.Errorf("refusing to append to invalid audit chain: %s", verification.Error)
	}
	rawDetails, err := json.Marshal(details)
	if err != nil {
		return AuditRecord{}, fmt.Errorf("encode audit details: %w", err)
	}
	record := AuditRecord{
		Sequence: verification.Records + 1, Time: now.UTC(), Action: action, Actor: actor,
		Release: release, Details: rawDetails, Previous: verification.LastHash,
	}
	hash, err := auditHash(record)
	if err != nil {
		return AuditRecord{}, err
	}
	record.Hash = hash
	line, err := json.Marshal(record)
	if err != nil {
		return AuditRecord{}, err
	}
	file, err := os.OpenFile(s.AuditPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return AuditRecord{}, fmt.Errorf("open audit: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return AuditRecord{}, fmt.Errorf("append audit: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return AuditRecord{}, fmt.Errorf("sync audit: %w", err)
	}
	if err := file.Close(); err != nil {
		return AuditRecord{}, err
	}
	return record, nil
}

func auditHash(record AuditRecord) (string, error) {
	copyRecord := record
	copyRecord.Hash = ""
	data, err := json.Marshal(copyRecord)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (s Store) VerifyAudit() (AuditVerification, error) {
	file, err := os.Open(s.AuditPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AuditVerification{Valid: true}, nil
		}
		return AuditVerification{}, fmt.Errorf("open audit: %w", err)
	}
	defer file.Close()
	verification := AuditVerification{Valid: true}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	previous := ""
	for scanner.Scan() {
		verification.Records++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			verification.Valid = false
			verification.BrokenLine = verification.Records
			verification.Error = "blank audit record"
			return verification, nil
		}
		var record AuditRecord
		if err := decodeStrictJSON(line, &record); err != nil {
			verification.Valid = false
			verification.BrokenLine = verification.Records
			verification.Error = err.Error()
			return verification, nil
		}
		if record.Sequence != verification.Records {
			verification.Valid = false
			verification.BrokenLine = verification.Records
			verification.Error = "sequence mismatch"
			return verification, nil
		}
		if record.Previous != previous {
			verification.Valid = false
			verification.BrokenLine = verification.Records
			verification.Error = "previous hash mismatch"
			return verification, nil
		}
		expected, hashErr := auditHash(record)
		if hashErr != nil {
			return AuditVerification{}, hashErr
		}
		if record.Hash != expected {
			verification.Valid = false
			verification.BrokenLine = verification.Records
			verification.Error = "record hash mismatch"
			return verification, nil
		}
		previous = record.Hash
	}
	if err := scanner.Err(); err != nil {
		return AuditVerification{}, fmt.Errorf("read audit: %w", err)
	}
	verification.LastHash = previous
	return verification, nil
}

func (s Store) ReadAudit(limit int) ([]AuditRecord, error) {
	file, err := os.Open(s.AuditPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []AuditRecord{}, nil
		}
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	records := []AuditRecord{}
	for {
		var record AuditRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		records = append(records, record)
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}
