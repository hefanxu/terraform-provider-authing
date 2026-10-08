package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"terraform-provider-authing/internal/authingapi"
	"testing"
)

type mockField struct {
	sync.Mutex
	model                                                   mockObject
	id, key                                                 string
	foreign, rows, failReplacement, foreignAfterReplacement bool
	creates, deletes, modelDeletes                          int
	paths                                                   []string
}

func (m *mockField) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	ok := func(d any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": d}) }
	bad := func() { http.Error(w, "unsafe", 500) }
	switch r.URL.Path {
	case "/api/v3/metadata/list-field":
		if r.Method != http.MethodGet || r.URL.Query().Get("modelId") != m.model.id || r.URL.Query().Get("from") != "terraform" {
			bad()
			return
		}
		fields := []any{}
		if m.id != "" {
			fields = append(fields, map[string]any{"id": m.id, "modelId": m.model.id, "key": m.key, "name": "Owned field", "type": 1, "show": true, "editable": true})
		}
		if m.foreign {
			fields = append(fields, map[string]any{"id": "foreign", "modelId": m.model.id, "key": "foreign", "type": 1})
		}
		ok(fields)
	case "/api/v3/metadata/create-field":
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil || m.model.id == "" || m.id != "" || b["modelId"] != m.model.id || b["key"] != fieldKey && b["key"] != fieldKey+"-new" || b["name"] != "Owned field" || b["type"] != "Text" || b["show"] != true || b["editable"] != true {
			bad()
			return
		}
		if m.failReplacement && b["key"] == fieldKey+"-new" {
			bad()
			return
		}
		m.key = b["key"].(string)
		m.id = fmt.Sprintf("field-owned-%d", m.creates+1)
		m.creates++
		if m.creates == 2 && m.foreignAfterReplacement {
			m.foreign = true
		}
		ok(map[string]any{"id": m.id, "modelId": m.model.id, "key": m.key})
	case "/api/v3/metadata/remove-field":
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil || b["id"] != m.id || b["modelId"] != m.model.id || m.id == "" {
			bad()
			return
		}
		m.id = ""
		m.key = ""
		m.deletes++
		ok(map[string]any{"success": true})
	case "/api/v3/metadata/filter":
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil || b["modelId"] != m.model.id || b["page"] != float64(1) || b["limit"] != float64(1) {
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
		if m.id != "" || m.foreign || m.rows {
			bad()
			return
		}
		m.modelDeletes++
		m.model.serve(w, r)
	default:
		m.model.serve(w, r)
	}
}
func fieldClient(t *testing.T, m *mockField) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	return c, s
}
func fieldCredentials(s *httptest.Server) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}
}
func TestMockFieldTerraformReplacement(t *testing.T) {
	m := &mockField{}
	_, s := fieldClient(t, m)
	defer s.Close()
	if e := runFieldTrace(t.TempDir(), fieldCredentials(s), objectTestCode); e != nil {
		t.Fatalf("%v paths=%v create=%d delete=%d", e, m.paths, m.creates, m.deletes)
	}
	if m.creates != 2 || m.deletes != 2 || m.modelDeletes != 1 || m.model.id != "" {
		t.Fatalf("replacement/teardown incomplete: create=%d delete=%d modelDelete=%d", m.creates, m.deletes, m.modelDeletes)
	}
}
func TestFieldCleanupRefusesForeignFieldsOrRows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*mockField)
	}{{"foreign field", func(m *mockField) { m.foreign = true }}, {"rows", func(m *mockField) { m.rows = true }}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockField{model: mockObject{id: "model-owned", name: objectTestCode}, id: "field-owned-1", key: fieldKey}
			tc.mutate(m)
			c, s := fieldClient(t, m)
			defer s.Close()
			if cleanupFieldModel(c, objectTestCode, "model-owned", "field-owned-1", fieldKey) == nil || m.deletes != 0 || m.modelDeletes != 0 {
				t.Fatal("deleted with foreign dependencies")
			}
		})
	}
}
func TestFieldTraceRefusesExistingModel(t *testing.T) {
	m := &mockField{model: mockObject{id: "model-owned", name: objectTestCode}}
	_, s := fieldClient(t, m)
	defer s.Close()
	if runFieldTrace(t.TempDir(), fieldCredentials(s), objectTestCode) == nil || m.creates != 0 || m.modelDeletes != 0 {
		t.Fatal("adopted or deleted existing model")
	}
}

func TestFieldTraceStopsBeforeDeletingWhenForeignFieldAppears(t *testing.T) {
	m := &mockField{foreignAfterReplacement: true}
	_, s := fieldClient(t, m)
	defer s.Close()
	err := runFieldTrace(t.TempDir(), fieldCredentials(s), objectTestCode)
	if err == nil || m.deletes != 1 || m.modelDeletes != 0 {
		t.Fatalf("unexpected destructive cleanup: err=%v deletes=%d modelDeletes=%d", err, m.deletes, m.modelDeletes)
	}
}

func TestFieldFailedReplacementScrubsError(t *testing.T) {
	m := &mockField{failReplacement: true}
	_, s := fieldClient(t, m)
	defer s.Close()
	e := runFieldTrace(t.TempDir(), fieldCredentials(s), objectTestCode)
	if e == nil {
		t.Fatal("expected failed replacement")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "mock-token"} {
		if strings.Contains(e.Error(), secret) {
			t.Fatal("leaked value")
		}
	}
}
