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

const pipelineTestCode = "hermesacc-fedcba9876543210"

type mockPipeline struct {
	sync.Mutex
	id, name                                            string
	sourceForeign, sceneForeign, failUpdate, failCreate bool
	creates, deletes, updates                           int
	paths                                               []string
}

func (m *mockPipeline) serve(w http.ResponseWriter, r *http.Request) {
	m.Lock()
	defer m.Unlock()
	m.paths = append(m.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	ok := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": data}) }
	if r.URL.Path == "/api/v3/get-management-token" {
		ok(map[string]any{"access_token": "mock-token", "expires_in": 3600})
		return
	}
	if r.URL.Path == "/api/v3/get-pipeline-function" {
		if r.Method != http.MethodGet || r.URL.Query().Get("funcId") != m.id || m.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		src := pipelineInertSource
		if m.sourceForeign {
			src = "foreign"
		}
		scene := pipelineScene
		if m.sceneForeign {
			scene = "POST_REGISTER"
		}
		ok(map[string]any{"funcId": m.id, "funcName": m.name, "funcDescription": pipelineMarker(pipelineTestCode), "scene": scene, "sourceCode": src, "isAsynchronous": false, "enabled": false})
		return
	}
	var b map[string]any
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&b) != nil {
		http.Error(w, "bad request", 500)
		return
	}
	switch r.URL.Path {
	case "/api/v3/create-pipeline-function":
		if m.failCreate {
			http.Error(w, "source-secret-marker", 500)
			return
		}
		if m.id != "" || b["funcName"] != pipelineTestCode || b["funcDescription"] != pipelineMarker(pipelineTestCode) || b["scene"] != pipelineScene || b["sourceCode"] != pipelineInertSource || b["isAsynchronous"] != false || b["enabled"] != false {
			http.Error(w, "unsafe create", 500)
			return
		}
		m.id, m.name = "pipeline-owned", pipelineTestCode
		m.creates++
		ok(map[string]any{"funcId": m.id})
	case "/api/v3/update-pipeline-function":
		if m.failUpdate {
			http.Error(w, "source-secret-marker", 500)
			return
		}
		if b["funcId"] != m.id || m.id == "" || b["sourceCode"] != pipelineInertSource || b["isAsynchronous"] != false || b["enabled"] != false || b["funcName"] != pipelineTestCode && b["funcName"] != pipelineTestCode+"-drift" {
			http.Error(w, "unsafe update", 500)
			return
		}
		m.name = b["funcName"].(string)
		m.updates++
		ok(map[string]any{"funcId": m.id})
	case "/api/v3/delete-pipeline-function":
		if m.id == "" || b["funcId"] != m.id {
			http.Error(w, "unsafe delete", 500)
			return
		}
		m.id = ""
		m.deletes++
		ok(map[string]any{"success": true})
	default:
		http.Error(w, "unexpected", 500)
	}
}
func pipelineClient(t *testing.T, m *mockPipeline) (*authingapi.Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serve))
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	return c, s
}
func TestMockPipelineTerraformTrace(t *testing.T) {
	m := &mockPipeline{}
	_, s := pipelineClient(t, m)
	defer s.Close()
	err := runPipelineTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, pipelineTestCode)
	if err != nil {
		t.Fatalf("%v paths=%v creates=%d", err, m.paths, m.creates)
	}
	if m.id != "" || m.creates != 1 || m.updates < 2 || m.deletes != 1 {
		t.Fatalf("lifecycle create=%d update=%d delete=%d", m.creates, m.updates, m.deletes)
	}
}
func TestPipelineCleanupRefusesForeignSourceOrScene(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*mockPipeline)
	}{{"source", func(m *mockPipeline) { m.sourceForeign = true }}, {"scene", func(m *mockPipeline) { m.sceneForeign = true }}} {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockPipeline{id: "pipeline-owned", name: pipelineTestCode}
			tc.mutate(m)
			c, s := pipelineClient(t, m)
			defer s.Close()
			if cleanupPipeline(c, "pipeline-owned", pipelineTestCode) == nil || m.deletes != 0 {
				t.Fatal("unsafe cleanup")
			}
		})
	}
}
func TestPipelineFailedCreateRefusesGuessedDeletion(t *testing.T) {
	m := &mockPipeline{failCreate: true}
	_, s := pipelineClient(t, m)
	defer s.Close()
	err := runPipelineTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, pipelineTestCode)
	if err == nil || !strings.Contains(err.Error(), "cleanup-incomplete") || strings.Contains(err.Error(), "source-secret-marker") || m.deletes != 0 {
		t.Fatalf("failed create cleanup boundary: %v", err)
	}
}

func TestPipelineFailureDoesNotLeakSource(t *testing.T) {
	m := &mockPipeline{failUpdate: true}
	_, s := pipelineClient(t, m)
	defer s.Close()
	err := runPipelineTrace(t.TempDir(), map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": s.URL}, pipelineTestCode)
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, secret := range []string{"key-marker", "credential-marker", "source-secret-marker", pipelineInertSource} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("leaked sensitive value")
		}
	}
}
