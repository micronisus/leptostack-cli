package kubernetes

import (
	"encoding/json"
	"testing"
)

// kustomizationJSON mirrors the real Flux API response, including fields the CLI
// does not model (history, inventory, observedGeneration) and the string-typed
// status.lastAppliedRevision that previously broke decoding.
const kustomizationJSON = `{
  "apiVersion": "kustomize.toolkit.fluxcd.io/v1",
  "kind": "Kustomization",
  "metadata": {
    "name": "cert-manager",
    "namespace": "flux-system",
    "creationTimestamp": "2026-10-05T14:22:28Z",
    "generation": 3,
    "labels": {"kustomize.toolkit.fluxcd.io/name": "flux-system"}
  },
  "spec": {
    "interval": "10m0s",
    "path": "./apps/devops/base/cert-manager",
    "prune": true,
    "wait": true,
    "force": false,
    "timeout": "5m0s",
    "sourceRef": {"kind": "GitRepository", "name": "flux-system"}
  },
  "status": {
    "observedGeneration": 3,
    "lastAppliedRevision": "main@sha1:bf1ec7e8fd05096dc60bfa5bd0b7feb9eb6d30b0",
    "lastAttemptedRevision": "main@sha1:bf1ec7e8fd05096dc60bfa5bd0b7feb9eb6d30b0",
    "history": [{"revision": "main@sha1:abc", "status": "True"}],
    "inventory": {"entries": [{"id": "cert-manager_cert-manager__Certificate", "v": "v1"}]},
    "conditions": [
      {"type": "Ready", "status": "True", "reason": "ReconciliationSucceeded", "message": "Applied revision: main@sha1:bf1ec7"},
      {"type": "Healthy", "status": "True", "message": "Reconciliation succeeded"}
    ]
  }
}`

func TestKustomizationListUnmarshal(t *testing.T) {
	var list KustomizationList
	payload := `{"apiVersion":"kustomize.toolkit.fluxcd.io/v1","kind":"KustomizationList","items":[` + kustomizationJSON + `]}`
	if err := json.Unmarshal([]byte(payload), &list); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("items = %d want 1", len(list.Items))
	}
	k := list.Items[0]
	if k.Metadata.Name != "cert-manager" {
		t.Errorf("name = %q", k.Metadata.Name)
	}
	if !k.Ready() || k.Suspended() {
		t.Errorf("ready=%t suspended=%t", k.Ready(), k.Suspended())
	}
	if k.Message() == "" {
		t.Error("expected a Ready message")
	}
	if k.Status.LastAppliedRevision == "" {
		t.Error("lastAppliedRevision should decode as a non-empty string")
	}
}

func TestSourceUnmarshalBucketAndOCI(t *testing.T) {
	bucket := `{"metadata":{"name":"assets"},"spec":{"endpoint":"https://s3.example.com","bucketName":"buck"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`
	var b Source
	if err := json.Unmarshal([]byte(bucket), &b); err != nil {
		t.Fatalf("bucket unmarshal: %v", err)
	}
	if b.Location() != "https://s3.example.com" {
		t.Errorf("bucket location = %q", b.Location())
	}
	if !b.Ready() {
		t.Error("bucket should be ready")
	}

	oci := `{"metadata":{"name":"charts"},"spec":{"url":"oci://ghcr.io/acme/charts","ref":{"tag":"1.2.3"}},"status":{"conditions":[{"type":"Ready","status":"False"}]}}`
	var o Source
	if err := json.Unmarshal([]byte(oci), &o); err != nil {
		t.Fatalf("oci unmarshal: %v", err)
	}
	if o.Location() != "oci://ghcr.io/acme/charts" {
		t.Errorf("oci location = %q", o.Location())
	}
	if o.RefString() != "tag=1.2.3" {
		t.Errorf("oci ref = %q", o.RefString())
	}
}
