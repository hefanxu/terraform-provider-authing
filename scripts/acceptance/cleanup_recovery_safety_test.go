package acceptance

import (
	"encoding/json"
	"testing"
)

func TestCleanupAuditRecoveryMatchesOnlyIndependentFullFingerprint(t *testing.T) {
	raw := json.RawMessage(`{"tenantId":"original-id","password":"private-secret","nested":{"organizationCode":"original-org"}}`)
	want := map[string]bool{cleanupHash("original-id"): true, cleanupHash("original-org"): true}
	got := map[string]string{}
	cleanupRecoverHashes(raw, want, got, 0)
	if len(got) != 2 || got[cleanupHash("original-id")] != "original-id" || got[cleanupHash("original-org")] != "original-org" {
		t.Fatal("fingerprint recovery failed")
	}
	if _, ok := got[cleanupHash("private-secret")]; ok {
		t.Fatal("unrequested value retained")
	}
}
func TestCleanupKnownFingerprintCannotBeHiddenByRenaming(t *testing.T) {
	var item map[string]json.RawMessage
	json.Unmarshal([]byte(`{"userId":"original-user-id","username":"renamed-business-looking-name"}`), &item)
	f := cleanupFamily{Family: "user", ID: "userId", Name: "username"}
	if !cleanupMatchesEvidence(f, item, []string{cleanupHash("original-user-id")}) || cleanupMatchesEvidence(f, item, []string{cleanupHash("another-id")}) {
		t.Fatal("known identity scan depends on mutable name")
	}
}
func TestCleanupAuditRecoveryCannotTreatTruncatedHashAsAuthority(t *testing.T) {
	got := map[string]string{}
	cleanupRecoverHashes(json.RawMessage(`{"tenantId":"original-id"}`), map[string]bool{cleanupHash("original-id")[:16]: true}, got, 0)
	if len(got) != 0 {
		t.Fatal("truncated evidence matched")
	}
}
