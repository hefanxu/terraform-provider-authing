package acceptance

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanupCandidateDoesNotPromoteDiscoveryOrExposePII(t *testing.T) {
	var item map[string]json.RawMessage
	json.Unmarshal([]byte(`{"userId":"private-user-id","username":"hermesacc-1234567890abcdef","nickname":"private-name","email":"private-email"}`), &item)
	e := cleanupCandidate(cleanupFamily{Family: "user", ID: "userId", Name: "username"}, item)
	raw, _ := json.Marshal(e)
	if e.Authority != "none" || e.Hash != cleanupHash("private-user-id") || e.Code != "hermesacc-1234567890abcdef" {
		t.Fatal("bad safe candidate")
	}
	if strings.Contains(string(raw), "private-") {
		t.Fatal("PII exposed")
	}
}
func TestCleanupInventoryLiveGuard(t *testing.T) {
	if cleanupLiveGuard(false, "READ_ONLY_SANDBOX", "key", "secret") || cleanupLiveGuard(true, "DESTRUCTIVE_SANDBOX", "key", "secret") || cleanupLiveGuard(true, "READ_ONLY_SANDBOX", "", "secret") {
		t.Fatal("unsafe guard")
	}
	if !cleanupLiveGuard(true, "READ_ONLY_SANDBOX", "key", "secret") {
		t.Fatal("valid read-only guard rejected")
	}
}
