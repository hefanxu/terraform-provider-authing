package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

type mockObject struct {
	sync.Mutex
	id, name                                    string
	fields, rows, foreign, ambiguous, failDrift bool
	deletes                                     int
	paths                                       []string
}

func (m *mockObject) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	code := objectTestCode
	marker := objectMarker(code)
	var b map[string]json.RawMessage
	if r.Method == http.MethodPost && r.URL.Path != "/api/v3/get-management-token" {
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			http.Error(w, "bad request", 500)
			return
		}
	}
	str := func(k string) string { var s string; _ = json.Unmarshal(b[k], &s); return s }
	ok := func(d any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": d}) }
	bad := func() { http.Error(w, "unexpected request", 500) }
	model := func() any {
		description := marker
		if m.foreign {
			description = "foreign"
		}
		return map[string]any{"id": m.id, "name": m.name, "description": description, "type": "custom", "dataType": "list", "parentKey": "", "enable": true, "fieldOrder": "", "config": map[string]any{}}
	}
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/metadata/list-model":
		list := []any{}
		if m.id != "" {
			list = append(list, model())
		}
		if m.ambiguous {
			list = append(list, model())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "list": list})
	case "/api/v3/metadata/get-model":
		if r.URL.Query().Get("id") != m.id || m.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		ok(model())
	case "/api/v3/metadata/create-model":
		if m.id != "" || str("name") != code || str("description") != marker || str("type") != "custom" || str("dataType") != "list" {
			bad()
			return
		}
		m.id = "model-owned"
		m.name = code
		ok(model())
	case "/api/v3/metadata/update-model":
		if m.failDrift && str("name") == code+"-updated-drift" {
			http.Error(w, "secret-marker", 500)
			return
		}
		if str("id") != m.id || m.id == "" || str("description") != marker || str("showFieldKey") != "" {
			bad()
			return
		}
		m.name = str("name")
		ok(model())
	case "/api/v3/metadata/list-field":
		if r.URL.Query().Get("modelId") != m.id || r.URL.Query().Get("from") != "terraform" {
			bad()
			return
		}
		if m.fields {
			ok([]any{map[string]string{"id": "foreign"}})
		} else {
			ok([]any{})
		}
	case "/api/v3/metadata/filter":
		if str("modelId") != m.id || string(b["page"]) != "1" || string(b["limit"]) != "1" {
			bad()
			return
		}
		n := 0
		list := []any{}
		if m.rows {
			n = 1
			list = append(list, map[string]string{"rowId": "foreign"})
		}
		ok(map[string]any{"totalCount": n, "list": list})
	case "/api/v3/metadata/remove-model":
		if str("id") != m.id || m.id == "" || m.fields || m.rows || m.foreign {
			bad()
			return
		}
		m.id = ""
		m.deletes++
		ok(map[string]any{"success": true})
	default:
		bad()
	}
}
func objectClient(t *testing.T, m *mockObject) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	c, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

const objectTestCode = "hermesacc-0123456789abcdef"

func TestMockObjectTerraformTrace(t *testing.T) {
	m := &mockObject{}
	_, s := objectClient(t, m)
	defer s.Close()
	err := runObjectTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, objectTestCode)
	if err != nil {
		t.Fatalf("%v paths=%v", err, m.paths)
	}
	if m.id != "" || m.deletes != 1 || strings.Count(strings.Join(m.paths, ","), "POST /api/v3/metadata/update-model") < 2 {
		t.Fatalf("incomplete lifecycle: %+v", m)
	}
}
func TestObjectCleanupRefusesDependenciesAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*mockObject)
	}{
		{"fields", func(m *mockObject) { m.fields = true }}, {"rows", func(m *mockObject) { m.rows = true }},
		{"foreign", func(m *mockObject) { m.foreign = true }}, {"ambiguous", func(m *mockObject) { m.ambiguous = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockObject{id: "model-owned", name: objectTestCode}
			tc.change(m)
			c, s := objectClient(t, m)
			defer s.Close()
			if cleanupObject(c, objectTestCode, "model-owned") == nil || m.deletes != 0 {
				t.Fatal("unsafe deletion")
			}
		})
	}
}
func TestObjectTracePreflightRefusesExisting(t *testing.T) {
	m := &mockObject{id: "model-owned", name: objectTestCode}
	_, s := objectClient(t, m)
	defer s.Close()
	if runObjectTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, objectTestCode) == nil {
		t.Fatal("adopted existing model")
	}
	if m.deletes != 0 {
		t.Fatal("deleted existing model")
	}
}
func TestObjectTraceFailedDriftCleansAndScrubsErrors(t *testing.T) {
	m := &mockObject{failDrift: true}
	_, s := objectClient(t, m)
	defer s.Close()
	err := runObjectTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, objectTestCode)
	if err == nil {
		t.Fatal("accepted failed drift")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token", "secret-marker"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error disclosed credential or API body")
		}
	}
	if m.id != "" || m.deletes != 1 {
		t.Fatalf("failure left model behind: %+v", m)
	}
}

func TestDestructiveLiveObjectTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	code, err := newGroupCode()
	if err != nil {
		t.Fatal("model code generation failed")
	}
	if err = runObjectTrace(t.TempDir(), env, code); err != nil {
		t.Fatal(err)
	}
}
