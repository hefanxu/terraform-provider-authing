package acceptance

import (
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

	"terraform-provider-authing/internal/authingapi"
)

// Invitation policies have no description/marker field. A random name, a
// pre-create empty listing, the discovered ID and exact GET establish ownership.
func invitationRequest(client *authingapi.Client, path, method string, payload any) (struct {
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}, error) {
	var out struct {
		StatusCode int             `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	raw, err := client.SendHttpRequest(path, method, payload)
	if err != nil {
		return out, errors.New("invitation API request failed")
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, errors.New("invalid invitation API response")
	}
	return out, nil
}

func findInvitationPolicy(client *authingapi.Client, name string) (string, error) {
	var match string
	seen := 0
	for page := 1; page <= 100; page++ {
		out, err := invitationRequest(client, "/api/v3/list-invitation-policies", http.MethodPost, map[string]any{"page": page, "limit": 50, "keywords": name})
		if err != nil || out.StatusCode != 200 {
			return "", errors.New("policy listing failed")
		}
		var data struct {
			TotalCount *int `json:"totalCount"`
			List       []struct {
				ID   string `json:"policyId"`
				Name string `json:"name"`
			} `json:"list"`
		}
		if json.Unmarshal(out.Data, &data) != nil || data.TotalCount == nil || *data.TotalCount < 0 || data.List == nil || seen+len(data.List) > *data.TotalCount || len(data.List) > 50 {
			return "", errors.New("invalid policy listing")
		}
		for _, row := range data.List {
			if row.ID == "" || row.Name == "" {
				return "", errors.New("invalid policy row")
			}
			if row.Name == name || row.Name == name+"-drift" {
				if match != "" {
					return "", errors.New("ambiguous policy name")
				}
				match = row.ID
			}
		}
		seen += len(data.List)
		if seen == *data.TotalCount {
			return match, nil
		}
		if len(data.List) == 0 {
			return "", errors.New("incomplete policy listing")
		}
	}
	return "", errors.New("policy listing exceeded page limit")
}

func invitationPolicyDetail(client *authingapi.Client, id string) (string, bool, error) {
	if id == "" {
		return "", false, errors.New("missing policy ID")
	}
	out, err := invitationRequest(client, "/api/v3/get-invitation-policy", http.MethodGet, map[string]string{"policyId": id})
	if err != nil {
		return "", false, err
	}
	if out.StatusCode == 404 {
		return "", false, nil
	}
	if out.StatusCode != 200 {
		return "", false, errors.New("policy GET failed")
	}
	var data struct {
		ID         string `json:"policyId"`
		Name       string `json:"name"`
		Identifier *bool  `json:"enabledIdentifierVerify"`
		Info       *bool  `json:"enabledInfoFill"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.ID != id || data.Name == "" || data.Identifier == nil || data.Info == nil || *data.Identifier || *data.Info {
		return "", false, errors.New("policy identity or non-sending fixture not verified")
	}
	return data.Name, true, nil
}

func ownedInvitationPolicy(client *authingapi.Client, id, name string) error {
	if !sandboxCode.MatchString(name) || id == "" {
		return errors.New("invalid generated policy identity")
	}
	got, found, err := invitationPolicyDetail(client, id)
	if err != nil || !found || got != name && got != name+"-drift" {
		return errors.New("policy ownership not verified")
	}
	return nil
}

// The policy-scoped roster endpoint returns a count and list. Refuse deletion
// unless the complete scoped inventory proves zero assignments; never unbind.
func noInvitationRosters(client *authingapi.Client, id string) error {
	out, err := invitationRequest(client, "/api/v3/list-invitation-rosters-by-policy-id", http.MethodPost, map[string]any{"policyId": id, "page": 1, "limit": 50, "withAssignedPolicy": true, "withRosterSecret": false})
	if err != nil || out.StatusCode != 200 {
		return errors.New("roster inventory failed")
	}
	var data struct {
		TotalCount *int              `json:"totalCount"`
		List       []json.RawMessage `json:"list"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.TotalCount == nil || *data.TotalCount != 0 || data.List == nil || len(data.List) != 0 {
		return errors.New("roster inventory not proved empty")
	}
	return nil
}

func cleanupInvitationPolicy(client *authingapi.Client, id, name string) error {
	if err := ownedInvitationPolicy(client, id, name); err != nil {
		_, found, getErr := invitationPolicyDetail(client, id)
		if getErr == nil && !found && sandboxCode.MatchString(name) {
			return nil
		}
		return err
	}
	listed, err := findInvitationPolicy(client, name)
	if err != nil || listed != id {
		return errors.New("policy listing does not confirm owned ID")
	}
	if err := noInvitationRosters(client, id); err != nil {
		return err
	}
	out, err := invitationRequest(client, "/api/v3/delete-invitation-policies-batch", http.MethodPost, map[string]any{"policyIds": []string{id}})
	if err != nil || out.StatusCode != 200 {
		return errors.New("policy delete rejected")
	}
	var data struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(out.Data, &data) != nil || data.Success == nil || !*data.Success {
		return errors.New("policy deletion not confirmed")
	}
	_, found, err := invitationPolicyDetail(client, id)
	if err != nil || found {
		return errors.New("policy GET 404 not confirmed")
	}
	return nil
}

func invitationStateID(root string, env []string, terraform string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.Output()
	if err != nil {
		return "", errors.New("policy state inspection failed")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return "", errors.New("invalid policy state")
	}
	for _, r := range state.Values.RootModule.Resources {
		if r.Address == "authing_invitation_policy.sandbox" && r.Values.ID != "" && sandboxCode.MatchString(r.Values.Name) {
			return r.Values.ID, nil
		}
	}
	return "", errors.New("policy ID absent from state")
}

func runInvitationPolicyTrace(root string, credentials map[string]string, name string) (result error) {
	if !sandboxCode.MatchString(name) {
		return errors.New("policy tracer requires generated hermesacc name")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("invitation-policy phase=terraform-cli code=%s (output suppressed)", name)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("invitation-policy phase=go-toolchain code=%s (output suppressed)", name)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("invitation-policy phase=client code=%s (output suppressed)", name)
	}
	existing, err := findInvitationPolicy(client, name)
	if err != nil || existing != "" {
		return fmt.Errorf("invitation-policy phase=preflight code=%s (output suppressed)", name)
	}
	started := false
	id := ""
	defer func() {
		if !started {
			return
		}
		discovered, e := findInvitationPolicy(client, name)
		if e != nil || id != "" && discovered != "" && discovered != id {
			result = fmt.Errorf("invitation-policy phase=cleanup-incomplete code=%s id=%s (output suppressed)", name, id)
			return
		}
		if discovered != "" {
			id = discovered
		}
		if id == "" {
			if result != nil {
				result = fmt.Errorf("invitation-policy phase=cleanup-incomplete code=%s (output suppressed)", name)
			}
			return
		}
		if e = cleanupInvitationPolicy(client, id, name); e != nil {
			result = fmt.Errorf("invitation-policy phase=cleanup-incomplete code=%s id=%s (output suppressed)", name, id)
		}
	}()
	providerDir, exampleDir := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("invitation-policy phase=workspace code=%s (output suppressed)", name)
	}
	build := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("invitation-policy phase=provider-build code=%s (output suppressed)", name)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	// No recipient, roster, link generation, invitation send, or password settings.
	hcl := fmt.Sprintf(`terraform {
 required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_invitation_policy" "sandbox" {
 name = %q
 enabled_identifier_verify = false
 enabled_info_fill = false
}
`, source, name)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("invitation-policy phase=config code=%s (output suppressed)", name)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	started = true
	return (traceCase{name: "invitation-policy", code: name, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-id-and-ownership", func() error {
			var e error
			id, e = invitationStateID(root, env, terraform)
			if e != nil {
				return e
			}
			listed, e := findInvitationPolicy(client, name)
			if e != nil || listed != id {
				return errors.New("created policy ID not uniquely listed")
			}
			return ownedInvitationPolicy(client, id, name)
		}},
		{"plan-converged", plan(0)},
		{"remote-drift", func() error {
			if e := ownedInvitationPolicy(client, id, name); e != nil {
				return e
			}
			out, e := invitationRequest(client, "/api/v3/update-invitation-policy", http.MethodPost, map[string]any{"policyId": id, "name": name + "-drift"})
			if e != nil || out.StatusCode != 200 {
				return errors.New("policy drift mutation failed")
			}
			got, found, e := invitationPolicyDetail(client, id)
			if e != nil || !found || got != name+"-drift" {
				return errors.New("policy drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-before-destroy", func() error {
			got, found, e := invitationPolicyDetail(client, id)
			if e != nil || !found || got != name {
				return errors.New("policy not reconciled")
			}
			if e = ownedInvitationPolicy(client, id, name); e != nil {
				return e
			}
			listed, e := findInvitationPolicy(client, name)
			if e != nil || listed != id {
				return errors.New("policy not uniquely owned")
			}
			return noInvitationRosters(client, id)
		}},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			_, found, e := invitationPolicyDetail(client, id)
			if e != nil || found {
				return errors.New("policy GET 404 not confirmed")
			}
			return nil
		}},
	}}).execute()
}

const invitationTestName = "hermesacc-1234567890abcdef"

type mockInvitationPolicy struct {
	sync.Mutex
	id, name                                                 string
	paths                                                    []string
	deletes                                                  int
	failDrift, failReadback, rosters, invalidRoster, foreign bool
}

func (m *mockInvitationPolicy) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	var body map[string]any
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid JSON", 500)
			return
		}
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/list-invitation-policies":
		if r.Method != http.MethodPost || body["page"] != float64(1) || body["limit"] != float64(50) || body["keywords"] != invitationTestName {
			http.Error(w, "bad list", 500)
			return
		}
		list := []any{}
		if m.id != "" {
			list = append(list, m.data())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": len(list), "list": list}})
	case "/api/v3/create-invitation-policy":
		if r.Method != http.MethodPost || m.id != "" || body["name"] != invitationTestName || body["enabledIdentifierVerify"] != false || body["enabledInfoFill"] != false || len(body) != 3 {
			http.Error(w, "bad create", 500)
			return
		}
		m.id = "mock-policy-id"
		m.name = invitationTestName
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/get-invitation-policy":
		if r.Method != http.MethodGet || r.URL.Query().Get("policyId") != m.id || m.id == "" || m.failReadback {
			m.failReadback = false
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/update-invitation-policy":
		if m.failDrift {
			http.Error(w, "secret-marker", 500)
			return
		}
		if r.Method != http.MethodPost || body["policyId"] != m.id || (body["name"] != invitationTestName && body["name"] != invitationTestName+"-drift") {
			http.Error(w, "bad update", 500)
			return
		}
		m.name = body["name"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": m.data()})
	case "/api/v3/list-invitation-rosters-by-policy-id":
		if r.Method != http.MethodPost || body["policyId"] != m.id || body["page"] != float64(1) || body["limit"] != float64(50) || body["withAssignedPolicy"] != true || body["withRosterSecret"] != false {
			http.Error(w, "bad roster scope", 500)
			return
		}
		if m.invalidRoster {
			fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
			return
		}
		count := 0
		if m.rosters {
			count = 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": count, "list": []any{}}})
	case "/api/v3/delete-invitation-policies-batch":
		ids, ok := body["policyIds"].([]any)
		if r.Method != http.MethodPost || !ok || len(ids) != 1 || ids[0] != m.id || m.id == "" || m.rosters || m.foreign {
			http.Error(w, "unowned deletion", 500)
			return
		}
		m.id = ""
		m.deletes++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected API path", 404)
	}
}
func (m *mockInvitationPolicy) data() map[string]any {
	name := m.name
	if m.foreign {
		name = "foreign policy"
	}
	return map[string]any{"policyId": m.id, "name": name, "enabledIdentifierVerify": false, "enabledInfoFill": false}
}
func invitationMockClient(t *testing.T, m *mockInvitationPolicy) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: server.URL})
	if err != nil {
		server.Close()
		t.Fatal("mock client failed")
	}
	return client, server
}
func TestMockInvitationPolicyTerraformTrace(t *testing.T) {
	m := &mockInvitationPolicy{}
	_, server := invitationMockClient(t, m)
	defer server.Close()
	err := runInvitationPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	defer m.Unlock()
	if m.id != "" || m.deletes != 1 {
		t.Fatal("policy not deleted exactly once")
	}
	counts := map[string]int{}
	for _, p := range m.paths {
		counts[p]++
	}
	for _, p := range []string{"POST /api/v3/create-invitation-policy", "GET /api/v3/get-invitation-policy", "POST /api/v3/list-invitation-rosters-by-policy-id", "POST /api/v3/delete-invitation-policies-batch"} {
		if counts[p] == 0 {
			t.Errorf("missing %s", p)
		}
	}
	if counts["POST /api/v3/update-invitation-policy"] < 2 {
		t.Fatal("drift was not reconciled")
	}
	for _, p := range m.paths {
		if strings.Contains(p, "send-invitation") || strings.Contains(p, "generate-invitation") {
			t.Fatal("one-shot invitation endpoint called")
		}
	}
}
func TestMockInvitationPolicyCleanupRefusesUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		foreign, rosters, invalid bool
	}{{"foreign", true, false, false}, {"assigned-roster", false, true, false}, {"incomplete-inventory", false, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockInvitationPolicy{id: "mock-policy-id", name: invitationTestName, foreign: tc.foreign, rosters: tc.rosters, invalidRoster: tc.invalid}
			client, server := invitationMockClient(t, m)
			defer server.Close()
			if cleanupInvitationPolicy(client, m.id, invitationTestName) == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if m.deletes != 0 || m.id == "" {
				t.Fatal("deleted unsafe policy")
			}
		})
	}
}
func TestMockInvitationPolicyRefusesExisting(t *testing.T) {
	m := &mockInvitationPolicy{id: "existing-policy", name: invitationTestName}
	_, server := invitationMockClient(t, m)
	defer server.Close()
	err := runInvitationPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || m.deletes != 0 || m.id != "existing-policy" {
		t.Fatal("preflight accepted existing policy")
	}
}
func TestMockInvitationPolicyFailureCleanup(t *testing.T) {
	m := &mockInvitationPolicy{failDrift: true}
	_, server := invitationMockClient(t, m)
	defer server.Close()
	err := runInvitationPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil {
		t.Fatal("failed drift accepted")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "secret-marker", "mock-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("leaked response or credential")
		}
	}
	if m.id != "" || m.deletes != 1 {
		t.Fatal("owned policy not cleaned up")
	}
}
func TestMockInvitationPolicyFailedCreateReadbackCleanup(t *testing.T) {
	m := &mockInvitationPolicy{failReadback: true}
	_, server := invitationMockClient(t, m)
	defer server.Close()
	err := runInvitationPolicyTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": server.URL}, invitationTestName)
	if err == nil || !strings.Contains(err.Error(), "phase=apply-create") {
		t.Fatal("failed create readback was not reported")
	}
	if m.id != "" || m.deletes != 1 {
		t.Fatal("created policy with no Terraform state was not cleaned up")
	}
}

func TestDestructiveLiveInvitationPolicyTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	name, err := newGroupCode()
	if err != nil {
		t.Fatal("policy name generation failed")
	}
	if err := runInvitationPolicyTrace(t.TempDir(), env, name); err != nil {
		t.Fatal(err)
	}
}
