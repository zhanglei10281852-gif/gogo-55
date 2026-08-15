# ReleaseRail

ReleaseRail is an original, standard-library-only Go CLI and backend for planning and simulating release orchestration entirely offline. It validates strict JSON or a deliberately limited YAML subset, checks semantic dependency constraints and local artifacts, builds deterministic rollout waves, evaluates gates and health criteria, orders migrations, simulates apply/rollback transitions, persists recoverable state, and maintains a verifiable append-only audit hash chain.

**Independent project notice:** ReleaseRail is an independent project and is not affiliated with, endorsed by, sponsored by, or associated with any vendor, product, or organization with a similar name.

## Safety model

ReleaseRail performs no network operations and contains no network client. `apply` and `rollback` only mutate local simulation state in the selected `--state` directory. Migration `command` and `rollback` values are descriptive plan data; ReleaseRail never executes them. Artifact paths must remain beneath the manifest directory.

## Build and test

Requires Go 1.22.5 or a compatible newer Go toolchain.

```powershell
go test ./... -count=1
go build ./...
go vet ./...
go build -o releaserail.exe ./cmd/releaserail
```

The module has no third-party dependencies.

## Quick offline run

From the repository root:

```powershell
.\releaserail.exe validate --artifacts examples\release.yaml
.\releaserail.exe plan --state .smoke-state examples\release.yaml
.\releaserail.exe apply --state .smoke-state --env staging examples\release.yaml
.\releaserail.exe status --state .smoke-state
.\releaserail.exe verify --state .smoke-state examples\release.yaml
.\releaserail.exe report --state .smoke-state
.\releaserail.exe rollback --state .smoke-state --env staging --previous api=0.9.0 examples\release.yaml
.\releaserail.exe diff examples\release.yaml examples\release.yaml
```

Delete `.smoke-state` afterward if desired. It contains only local state, snapshots, a lock while mutations are active, and `audit.jsonl`.

## Commands

- `validate [--json] [--artifacts] <manifest>` validates schema, versions, dependency ranges/DAG, gates, rollout settings, health rules, migrations, and optionally bytes on disk.
- `plan [--state DIR] [--json] [--verify=true] <manifest>` builds a deterministic environment/component plan. Existing approvals and health samples in state influence decisions.
- `apply --env NAME [--state DIR] [--actor NAME] [--json] <manifest>` verifies artifacts and simulates all local transitions. Unsatisfied gates prevent application.
- `status [--state DIR] [--json]` displays persisted environment and component state.
- `verify [--state DIR] [--json] <manifest>` verifies SHA-256/size and the audit chain. `--audit-only` requires no manifest.
- `rollback --env NAME --previous component=version,... [--state DIR] [--plan-only] [--force] [--json] <manifest>` creates or applies a reverse dependency/migration plan locally. Unsafe plans require `--force`.
- `diff [--json] <before> <after>` produces deterministic semantic manifest differences.
- `report [--state DIR] [--json]` renders a release, state, and audit summary.

Run `releaserail help` for the compact usage display.

## Manifest formats

JSON decoding is strict: unknown fields and any trailing JSON value are rejected. YAML is parsed by ReleaseRail itself, not by a library. The supported subset includes:

- mappings and sequences using two-space indentation;
- sequence entries that begin inline mappings;
- plain, single-quoted, and JSON-style double-quoted scalars;
- booleans, nulls, decimal integers/floats;
- flow sequences and flow mappings;
- comments outside quoted values and document markers.

Tabs, duplicate keys, malformed indentation, aliases, anchors, tags, block scalars, and multiple documents are rejected. Use JSON when full YAML language compatibility is required.

## State, locking, recovery, and audit

Mutating operations take an exclusive create lock with bounded waiting and stale-lock recovery. State writes use a same-directory temporary file, synchronization, and rename replacement. Before replacement, the prior state is copied into a content-tagged timestamped snapshot under `snapshots/`. The backend exposes snapshot listing and recovery, while the CLI currently uses recovery through backend consumers.

Audit records are JSON Lines. Every record contains a sequence, previous hash, canonical record hash, timestamp, actor, action, release, and details. Appends are refused if the existing chain fails verification. The chain protects accidental or visible tampering; it is not a digital signature and does not defend against an attacker who can rewrite the entire state directory.

## Release semantics

Versions follow semantic version precedence, including prerelease identifiers. Ranges support exact comparisons, `!=`, `<`, `<=`, `>`, `>=`, comma/space conjunction, `||`, hyphen ranges, `~`, `^`, and `x`/`X`/`*` wildcards. Dependencies are topologically ordered with deterministic lexical tie-breaking; rollback reverses that order.

Rollout instance ordering is derived from SHA-256 over the configured seed, environment, component, and instance number. It is stable across runs and then partitioned by wave size. Health comparisons support `<`, `<=`, `>`, `>=`, `==`, and `!=`. Required missing samples fail apply; optional missing samples do not.

Approval gates count unique eligible approvers represented in state. Condition gates compare environment variables using `=`, `==`, `!=`, or substring `~=`. Artifact gates require every planned local artifact verification to pass.

## Caveats

The YAML implementation intentionally supports only the documented subset. File locking is cooperative and based on exclusive lock-file creation rather than an operating-system advisory lock. Atomic replacement depends on local filesystem rename guarantees. Floating-point health comparisons are direct. Audit hashes provide integrity linkage, not identity authentication. ReleaseRail is an offline simulator and intentionally does not deploy, invoke migration commands, query monitoring systems, or contact approval services.
