package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

// A direct role-member list, never effective get-user-roles, proves the grant.
// Count and every page must agree even if the target appeared on page one.
func directRoleMember(client *authingapi.Client, namespace, role, user string) (bool, error) {
	seen := map[string]bool{}
	total := -1
	found := false
	for page := 1; page <= 10000; page++ {
		raw, err := client.SendHttpRequestContext(context.Background(), "/api/v3/list-role-members", http.MethodGet, map[string]any{"code": role, "namespace": namespace, "page": page, "limit": 50})
		var out struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				Total *int `json:"totalCount"`
				List  *[]struct {
					UserID string `json:"userId"`
				} `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(raw, &out) != nil || out.StatusCode != 200 || out.Data == nil || out.Data.Total == nil || out.Data.List == nil || *out.Data.Total < 0 {
			return false, errors.New("direct member inventory incomplete")
		}
		if total < 0 {
			total = *out.Data.Total
		}
		if total != *out.Data.Total {
			return false, errors.New("direct member count changed")
		}
		expected := total - len(seen)
		if expected > 50 {
			expected = 50
		}
		if len(*out.Data.List) != expected {
			return false, errors.New("direct member page incomplete")
		}
		for _, item := range *out.Data.List {
			if item.UserID == "" || seen[item.UserID] {
				return false, errors.New("direct member identity invalid")
			}
			seen[item.UserID] = true
			if item.UserID == user {
				found = true
			}
		}
		if len(seen) == total {
			return found, nil
		}
	}
	return false, errors.New("direct member pagination unbounded")
}
func emptyRoleMembers(c *authingapi.Client, ns, role string) error {
	raw, err := c.SendHttpRequestContext(context.Background(), "/api/v3/list-role-members", http.MethodGet, map[string]any{"code": role, "namespace": ns, "page": 1, "limit": 50})
	var out struct {
		StatusCode int `json:"statusCode"`
		Data       *struct {
			Total *int               `json:"totalCount"`
			List  *[]json.RawMessage `json:"list"`
		} `json:"data"`
	}
	if err != nil || json.Unmarshal(raw, &out) != nil || out.StatusCode != 200 || out.Data == nil || out.Data.Total == nil || *out.Data.Total != 0 || out.Data.List == nil || len(*out.Data.List) != 0 {
		return errors.New("role may retain direct members")
	}
	return nil
}

func verifyRoleMember(c *authingapi.Client, ns, role, user string, want bool) error {
	got, err := directRoleMember(c, ns, role, user)
	if err != nil || got != want {
		return errors.New("direct grant not verified")
	}
	return nil
}
func relationRoleState(root, tf string, env []string, ns, role, user string) error {
	cmd := exec.Command(tf, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New("state unavailable")
	}
	var state struct {
		Values struct {
			Root struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID         string `json:"id"`
						Code       string `json:"code"`
						RoleCode   string `json:"role_code"`
						Namespace  string `json:"namespace"`
						TargetType string `json:"target_type"`
						TargetID   string `json:"target_id"`
						Username   string `json:"username"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return errors.New("invalid state")
	}
	found := map[string]bool{}
	for _, r := range state.Values.Root.Resources {
		if found[r.Address] {
			return errors.New("duplicate state")
		}
		found[r.Address] = true
		switch r.Address {
		case "authing_namespace.sandbox":
			if r.Values.ID != ns || r.Values.Code != ns {
				return errors.New("namespace state mismatch")
			}
		case "authing_role.sandbox":
			if r.Values.Code != role || r.Values.Namespace != ns {
				return errors.New("role state mismatch")
			}
		case "authing_user.sandbox":
			if r.Values.ID != user || r.Values.Username == "" {
				return errors.New("user state mismatch")
			}
		case "authing_role_assignment.sandbox":
			if r.Values.ID != role+":USER:"+user || r.Values.Namespace != ns || r.Values.RoleCode != role || r.Values.TargetType != "USER" || r.Values.TargetID != user {
				return errors.New("relation state mismatch")
			}
		default:
			return errors.New("unexpected state resource")
		}
	}
	if len(found) != 4 {
		return errors.New("incomplete relation state")
	}
	return nil
}
func runRoleAssignmentTrace(root string, creds map[string]string, ns, username string) (result error) {
	if !sandboxCode.MatchString(ns) || !sandboxCode.MatchString(username) {
		return errors.New("invalid generated identities")
	}
	role := childCode(ns)
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	tf := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	goBin, goErr := exec.LookPath("go")
	if goErr != nil {
		goBin = filepath.Join(repo, "../.tools/go/bin/go")
	}
	if _, err := os.Stat(tf); err != nil {
		return errors.New("Terraform CLI unavailable")
	}
	if _, err := os.Stat(goBin); err != nil {
		return errors.New("Go unavailable")
	}
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: creds["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: creds["AUTHING_ACCESS_KEY_SECRET"], Host: creds["AUTHING_HOST"]})
	if err != nil {
		return errors.New("client unavailable")
	}
	n := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
	if n == nil || n.StatusCode != 404 {
		return errors.New("namespace preflight failed")
	}
	if err := verifyChildAbsent(c, ns, "role"); err != nil {
		return errors.New("role preflight failed")
	}
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return errors.New("workspace unavailable")
	}
	cmd := exec.Command(goBin, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	cmd.Dir = repo
	if _, err := cmd.CombinedOutput(); err != nil {
		return errors.New("provider build failed")
	}
	rc := fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
 required_providers {
  authing = { source = %q }
 }
}
provider "authing" {}
resource "authing_namespace" "sandbox" {
 code = %q
 name = %q
 description = %q
}
resource "authing_role" "sandbox" {
 code = %q
 name = %q
 namespace = authing_namespace.sandbox.code
 description = %q
}
resource "authing_user" "sandbox" {
 username = %q
 nickname = %q
}
resource "authing_role_assignment" "sandbox" {
 role_code = authing_role.sandbox.code
 namespace = authing_namespace.sandbox.code
 target_type = "USER"
 target_id = authing_user.sandbox.id
}
`, source, ns, ns, "hermesacc ownership "+ns, role, role, childMarker(ns), username, username)
	config := filepath.Join(root, "terraform.rc")
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return errors.New("config unavailable")
	}
	env := traceEnvironment(root, config, creds)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, tf, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	user := ""
	cleanupLabel := func() string {
		if result == nil {
			return "role-assignment phase=cleanup"
		}
		return result.Error()
	}
	defer func() {
		if user == "" {
			user, _ = userStateID(root, tf, env, username)
		}
		if user == "" {
			if result != nil {
				result = fmt.Errorf("%v; cleanup-unknown-user namespace=%s user=%s (output suppressed)", result, ns, username)
			}
			return
		}
		// Fail closed: never revoke a relation unless both parent identities remain owned.
		u := c.GetUser(&dto.GetUserDto{UserId: user})
		n := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
		if u != nil && u.StatusCode == 404 && n != nil && n.StatusCode == 404 {
			return
		}
		if u == nil || u.StatusCode != 200 || n == nil || n.StatusCode != 200 || verifyOwnedUser(c, user, username) != nil || verifyOwnedNamespaceIdentity(c, ns) != nil {
			result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
			return
		}
		exists, _, e := childExists(c, ns, "role")
		if e != nil {
			result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
			return
		}
		if exists {
			if verifyOwnedPermissionChild(c, ns, "role", false) != nil {
				result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
				return
			}
			present, e := directRoleMember(c, ns, role, user)
			if e != nil {
				result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
				return
			}
			if present {
				r := c.RevokeRole(&dto.RevokeRoleDto{Code: role, Namespace: ns, Targets: []dto.TargetDto{{TargetType: "USER", TargetIdentifier: user}}})
				if r == nil || r.StatusCode != 200 || !r.Data.Success {
					result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
					return
				}
			}
			if verifyRoleMember(c, ns, role, user, false) != nil || emptyRoleMembers(c, ns, role) != nil || cleanupPermissionChild(c, ns, "role") != nil {
				result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
				return
			}
		}
		if cleanupUser(c, user, username) != nil {
			result = fmt.Errorf("%s; cleanup-incomplete namespace=%s user=%s (output suppressed)", cleanupLabel(), ns, username)
		}
	}()
	result = (traceCase{name: "role-assignment", code: ns, phases: []tracePhase{
		{"apply-user", run(0, "apply", "-target=authing_user.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-user", func() error { var e error; user, e = userStateID(root, tf, env, username); return e }},
		{"apply-parents", run(0, "apply", "-target=authing_role.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-parents", func() error {
			if e := verifyOwnedUser(c, user, username); e != nil {
				return e
			}
			if e := verifyOwnedNamespaceIdentity(c, ns); e != nil {
				return e
			}
			return verifyOwnedPermissionChild(c, ns, "role", false)
		}},
		{"apply-relation", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-state", func() error { return relationRoleState(root, tf, env, ns, role, user) }},
		{"plan-converged", plan(0)},
		{"verify-direct-grant", func() error { return verifyRoleMember(c, ns, role, user, true) }},
		{"remote-revoke", func() error {
			r := c.RevokeRole(&dto.RevokeRoleDto{Code: role, Namespace: ns, Targets: []dto.TargetDto{{TargetType: "USER", TargetIdentifier: user}}})
			if r == nil || r.StatusCode != 200 || !r.Data.Success {
				return errors.New("remote revoke failed")
			}
			if e := verifyRoleMember(c, ns, role, user, false); e != nil {
				return e
			}
			return verifyOwnedUser(c, user, username)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-restored", func() error { return verifyRoleMember(c, ns, role, user, true) }},
		{"destroy-relation", run(0, "destroy", "-target=authing_role_assignment.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-user-preserved", func() error {
			if e := verifyRoleMember(c, ns, role, user, false); e != nil {
				return e
			}
			if e := emptyRoleMembers(c, ns, role); e != nil {
				return e
			}
			if e := verifyOwnedPermissionChild(c, ns, "role", false); e != nil {
				return e
			}
			if e := verifyChildNamespaceInventory(c, ns, "role", true); e != nil {
				return e
			}
			return verifyOwnedUser(c, user, username)
		}},
		{"destroy-parents", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absence", func() error {
			u := c.GetUser(&dto.GetUserDto{UserId: user})
			n := c.GetPermissionNamespace(&dto.GetPermissionNamespaceDto{Code: ns})
			if u == nil || u.StatusCode != 404 || n == nil || n.StatusCode != 404 {
				return errors.New("parent absence unverified")
			}
			return verifyChildAbsent(c, ns, "role")
		}},
	}}).execute()
	return result
}

type mockRoleRelation struct {
	sync.Mutex
	user       mockUser
	child      mockPermissionChild
	granted    bool
	events     []string
	incomplete bool
}

func (m *mockRoleRelation) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.events = append(m.events, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/api/v3/list-role-members":
		if r.URL.Query().Get("code") != childCode(namespaceTestCode) || r.URL.Query().Get("namespace") != namespaceTestCode || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
			http.Error(w, "scope", 500)
			return
		}
		list := []any{}
		if m.granted {
			list = append(list, map[string]string{"userId": m.user.id})
		}
		count := len(list)
		if m.incomplete {
			count++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": list}})
	case "/api/v3/assign-role", "/api/v3/revoke-role":
		var v struct {
			Code      string `json:"code"`
			Namespace string `json:"namespace"`
			Targets   []struct {
				TargetType       string `json:"targetType"`
				TargetIdentifier string `json:"targetIdentifier"`
			} `json:"targets"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.Code != childCode(namespaceTestCode) || v.Namespace != namespaceTestCode || len(v.Targets) != 1 || v.Targets[0].TargetType != "USER" || v.Targets[0].TargetIdentifier != m.user.id || m.user.id == "" || m.child.child == "" {
			http.Error(w, "invalid grant", 500)
			return
		}
		m.granted = r.URL.Path == "/api/v3/assign-role"
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	case "/api/v3/delete-roles-batch", "/api/v3/delete-permission-namespace", "/api/v3/delete-users-batch":
		if m.granted {
			http.Error(w, "grant still exists", 500)
			return
		}
		if strings.HasSuffix(r.URL.Path, "users-batch") {
			m.user.serve(w, r)
		} else {
			m.child.serve(w, r)
		}
	default:
		if strings.Contains(r.URL.Path, "user") {
			m.user.serve(w, r)
		} else {
			m.child.serve(w, r)
		}
	}
}
func TestMockRoleAssignmentTerraformLifecycle(t *testing.T) {
	m := &mockRoleRelation{child: mockPermissionChild{family: "role"}}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runRoleAssignmentTrace(t.TempDir(), mockUserCredentials(s.URL), namespaceTestCode, "hermesacc-1234567890abcdef")
	if err != nil {
		t.Fatalf("%v events=%v", err, m.events)
	}
	if m.granted || m.user.id != "" || m.child.child != "" || m.child.namespace != "" || m.user.deletes != 1 || m.child.childDeletes != 1 || m.child.namespaceDeletes != 1 {
		t.Fatal("lifecycle incomplete")
	}
	joined := strings.Join(m.events, ",")
	if strings.Count(joined, "POST /api/v3/assign-role") != 2 || strings.Count(joined, "POST /api/v3/revoke-role") != 2 || strings.Contains(joined, "get-user-roles") {
		t.Fatal("incorrect assignment lifecycle")
	}
	lastRevoke := strings.LastIndex(joined, "POST /api/v3/revoke-role")
	if strings.Index(joined, "POST /api/v3/delete-roles-batch") < lastRevoke || strings.Index(joined, "POST /api/v3/delete-users-batch") < lastRevoke {
		t.Fatal("parent deleted before relation")
	}
}
func TestMockRoleAssignmentIncompleteListFailsClosed(t *testing.T) {
	m := &mockRoleRelation{child: mockPermissionChild{family: "role"}, incomplete: true}
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	defer s.Close()
	err := runRoleAssignmentTrace(t.TempDir(), mockUserCredentials(s.URL), namespaceTestCode, "hermesacc-1234567890abcdef")
	if err == nil {
		t.Fatal("incomplete direct inventory accepted")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("credential leaked")
		}
	}
}
func TestDestructiveLiveRoleAssignmentTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires explicit destructive opt-in")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	ns, e := newNamespaceCode()
	if e != nil {
		t.Fatal("namespace generation failed")
	}
	user, e := newGroupCode()
	if e != nil || ns == user {
		t.Fatal("user generation failed")
	}
	if err := runRoleAssignmentTrace(t.TempDir(), env, ns, user); err != nil {
		t.Fatal(err)
	}
}
