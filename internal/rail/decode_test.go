package rail

import (
	"strings"
	"testing"
)

func TestStrictJSONRejectsUnknownAndTrailingData(t *testing.T) {
	valid := `{"name":"demo","version":"1.0.0","environments":[],"components":[],"policies":{}}`
	if _, err := DecodeManifest([]byte(valid), "json"); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	unknown := `{"name":"demo","version":"1.0.0","environments":[],"components":[],"policies":{},"mystery":true}`
	if _, err := DecodeManifest([]byte(unknown), "json"); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	if _, err := DecodeManifest([]byte(valid+` {}`), "json"); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing data error = %v", err)
	}
}

func TestYAMLSubsetDecodesNestedManifest(t *testing.T) {
	document := `
name: demo
version: 1.2.3
metadata: {owner: platform, tier: "one"}
environments:
  - name: staging
    rank: 1
    variables:
      ready: "yes"
    gates:
      - name: ready-check
        kind: condition
        condition: ready=yes
components:
  - name: api
    version: 1.2.3
    artifact:
      path: artifacts/api.bin
      sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      size: 5
    rollout:
      strategy: waves
      waveSize: 2
      instances: 3
    health:
      - name: errors
        metric: error_rate
        operator: <=
        threshold: 0.1
        required: true
policies:
  requireArtifact: true
`
	manifest, err := DecodeManifest([]byte(document), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "demo" || len(manifest.Components) != 1 || len(manifest.Environments) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if manifest.Components[0].Rollout.WaveSize != 2 {
		t.Fatalf("wave size = %d", manifest.Components[0].Rollout.WaveSize)
	}
}

func TestYAMLSubsetRejectsDuplicateKeysAndBadIndent(t *testing.T) {
	for _, document := range []string{
		"name: one\nname: two\n",
		"name: one\n version: 1.0.0\n",
		"items:\n  - one\n    child: bad\n",
	} {
		if _, err := DecodeManifest([]byte(document), "yaml"); err == nil {
			t.Errorf("invalid YAML unexpectedly accepted: %q", document)
		}
	}
}
