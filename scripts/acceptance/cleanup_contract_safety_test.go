package acceptance

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCleanupList404IsUnknownNotAbsence(t *testing.T) {
	c := cleanupMock(t, func(r *http.Request) any { return map[string]any{"statusCode": 404} })
	items, state := cleanupInventory(c, cleanupFamily{Family: "user_groups", Path: "/api/v3/get-user-groups", ID: "code"}, map[string]any{"userId": "mock-id"}, nil)
	if items != nil || state != "inventory-unavailable" {
		t.Fatalf("collection failure must not mean absence: %s", state)
	}
}
func TestCleanupIncompleteCountsAreUnknown(t *testing.T) {
	if cleanupCount(0, "inventory-unavailable") != -1 || cleanupCount(0, "complete") != 0 {
		t.Fatal("failed inventory advertised empty")
	}
}
func TestCleanupUnknownPathAndEmptyMethodRefused(t *testing.T) {
	calls := 0
	c := cleanupMock(t, func(r *http.Request) any { calls++; return map[string]any{"statusCode": 200} })
	_, _, state := cleanupRead(c, "/api/v3/not-published", "", nil)
	if state != "guard" || calls != 0 {
		t.Fatal("unknown path bypassed boundary")
	}
}
func TestCleanupPipelineRecoveryCoversAllPublishedScenes(t *testing.T) {
	scenes := map[string]bool{}
	for _, f := range cleanupFamilies() {
		if f.Family == "pipeline_function" {
			scene, _ := f.Extra["scene"].(string)
			scenes[scene] = true
		}
	}
	if len(scenes) != 10 || !scenes["AUTH_FLOW_FUNCTION"] || !scenes["PRE_AUTHENTICATION"] {
		t.Fatal("incomplete pipeline candidate search; never use it as auth-flow absence")
	}
}
func TestCleanupUserGroupsUsesPublishedUnpagedContract(t *testing.T) {
	c := cleanupMock(t, func(r *http.Request) any {
		switch r.URL.Path {
		case "/api/v3/get-user":
			return map[string]any{"statusCode": 200, "data": map[string]any{"userId": "mock-id", "username": "hermesacc-1234567890abcdef", "nickname": "hermesacc-1234567890abcdef"}}
		case "/api/v3/get-user-groups":
			if len(r.URL.Query()) != 1 || r.URL.Query().Get("userId") != "mock-id" {
				t.Error("unpublished pagination or scope")
			}
			return map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": 1, "list": []any{map[string]any{"code": "group-one"}}}}
		default:
			t.Error("unpublished user groups path")
			return map[string]any{"statusCode": 404}
		}
	})
	var item map[string]json.RawMessage
	json.Unmarshal([]byte(`{"userId":"mock-id","username":"hermesacc-1234567890abcdef"}`), &item)
	events := []cleanupEvent{}
	cleanupInspect(c, cleanupFamily{Family: "user", ID: "userId", Name: "username", Get: "/api/v3/get-user", GetID: "userId"}, item, func(e cleanupEvent) { events = append(events, e) })
	found := false
	for _, e := range events {
		if e.Kind == "dependency" && e.Family == "user_groups" {
			found = true
			if e.State != "complete" || e.Count != 1 {
				t.Fatal("incorrect membership diagnosis")
			}
		}
	}
	if !found {
		t.Fatal("missing groups dependency")
	}
}
