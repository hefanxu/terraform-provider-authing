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
	"time"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

// userStateID only trusts the single resource at the expected address and
// ownership username. Terraform output is parsed in memory, never reported.
func userStateID(root, terraform string, env []string, username string) (string, error) {
	cmd := exec.Command(terraform, "show", "-json")
	cmd.Dir = filepath.Join(root, "example")
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New("Terraform state unavailable")
	}
	var state struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string `json:"address"`
					Values  struct {
						ID       string `json:"id"`
						Username string `json:"username"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Values.RootModule.Resources) != 1 {
		return "", errors.New("Terraform state invalid")
	}
	resource := state.Values.RootModule.Resources[0]
	if resource.Address != "authing_user.sandbox" || resource.Values.Username != username || resource.Values.ID == "" {
		return "", errors.New("Terraform state ownership unverified")
	}
	return resource.Values.ID, nil
}

func verifyOwnedUser(client *authingapi.Client, id, username string) error {
	if id == "" || !sandboxCode.MatchString(username) {
		return errors.New("user ownership unverified")
	}
	got := client.GetUser(&dto.GetUserDto{UserId: id})
	if got == nil || got.StatusCode != 200 || got.Data.UserId != id || got.Data.Username != username || (got.Data.Nickname != username && got.Data.Nickname != username+"-drift") {
		return errors.New("user ownership unverified")
	}
	return nil
}

// Deletion is permitted only by a state-derived ID after an exact-ID GET.
func cleanupUser(client *authingapi.Client, id, username string) error {
	if id == "" || !sandboxCode.MatchString(username) {
		return errors.New("user ownership unverified")
	}
	for attempt := 0; attempt < 3; attempt++ {
		got := client.GetUser(&dto.GetUserDto{UserId: id})
		if got != nil && got.StatusCode == 404 {
			return nil
		}
		if got == nil || got.StatusCode != 200 || got.Data.UserId != id || got.Data.Username != username || (got.Data.Nickname != username && got.Data.Nickname != username+"-drift") {
			return errors.New("user ownership unverified")
		}
		client.DeleteUsersBatch(&dto.DeleteUsersBatchDto{UserIds: []string{id}})
		// Re-read before retrying: never delete without rechecking ownership.
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	got := client.GetUser(&dto.GetUserDto{UserId: id})
	if got != nil && got.StatusCode == 404 {
		return nil
	}
	return errors.New("user not confirmed absent")
}

func runUserTrace(root string, credentials map[string]string, username string) (result error) {
	if !sandboxCode.MatchString(username) {
		return errors.New("user tracer requires a generated hermesacc username")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return fmt.Errorf("user phase=terraform-cli username=%s (output suppressed)", username)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return fmt.Errorf("user phase=go-toolchain username=%s (output suppressed)", username)
		}
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: credentials["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: credentials["AUTHING_ACCESS_KEY_SECRET"], Host: credentials["AUTHING_HOST"]})
	if err != nil {
		return fmt.Errorf("user phase=client username=%s (output suppressed)", username)
	}
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return fmt.Errorf("user phase=workspace username=%s (output suppressed)", username)
	}
	binary := filepath.Join(providerDir, "terraform-provider-authing")
	build := exec.Command(goBinary, "build", "-o", binary, ".")
	build.Dir = repo
	if _, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("user phase=provider-build username=%s (output suppressed)", username)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	hcl := fmt.Sprintf(`terraform {
  required_providers { authing = { source = %q } }
}
provider "authing" {}
resource "authing_user" "sandbox" {
  username = %q
  nickname = %q
}
`, source, username, username)
	if os.WriteFile(config, []byte(rc), 0600) != nil || os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600) != nil {
		return fmt.Errorf("user phase=config username=%s (output suppressed)", username)
	}
	env := traceEnvironment(root, config, credentials)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, terraform, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	id := ""
	defer func() {
		if id == "" {
			id, _ = userStateID(root, terraform, env, username)
		}
		if id == "" {
			if result != nil {
				result = fmt.Errorf("user phase=cleanup-unknown-id username=%s (output suppressed)", username)
			}
			return
		}
		if cleanupUser(client, id, username) != nil {
			result = fmt.Errorf("user phase=cleanup-incomplete username=%s (output suppressed)", username)
		}
	}()
	result = (traceCase{name: "user", code: username, phases: []tracePhase{
		{"apply-create", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-id", func() error { var e error; id, e = userStateID(root, terraform, env, username); return e }},
		{"plan-converged", plan(0)},
		{"verify-created", func() error { return verifyOwnedUser(client, id, username) }},
		{"remote-drift", func() error {
			body, e := client.SendHttpRequestContext(context.Background(), "/api/v3/update-user", "POST", map[string]any{"userId": id, "nickname": username + "-drift"})
			var updated dto.UserSingleRespDto
			if e != nil || json.Unmarshal(body, &updated) != nil || updated.StatusCode != 200 || updated.Data.UserId != id || updated.Data.Nickname != username+"-drift" {
				return errors.New("drift mutation failed")
			}
			got := client.GetUser(&dto.GetUserDto{UserId: id})
			if got == nil || got.StatusCode != 200 || got.Data.UserId != id || got.Data.Username != username || got.Data.Nickname != username+"-drift" {
				return errors.New("drift not visible")
			}
			return nil
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-owned-before-destroy", func() error { return verifyOwnedUser(client, id, username) }},
		{"destroy", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absent", func() error {
			got := client.GetUser(&dto.GetUserDto{UserId: id})
			if got == nil || got.StatusCode != 404 {
				return errors.New("user not confirmed absent")
			}
			return nil
		}},
	}}).execute()
	return result
}

type mockUser struct {
	sync.Mutex
	id, username, nickname                           string
	paths                                            []string
	deletes                                          int
	failUpdate, failCreateAfterWrite, failDeleteOnce bool
	deleteAttempts                                   int
}

func (u *mockUser) serve(w http.ResponseWriter, r *http.Request) {
	u.Lock()
	defer u.Unlock()
	u.paths = append(u.paths, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v3/get-management-token":
		fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
	case "/api/v3/create-user":
		var v struct {
			Username string `json:"username"`
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || u.id != "" || v.Username != "hermesacc-1234567890abcdef" || v.Nickname != "hermesacc-1234567890abcdef" || v.Password != "" {
			http.Error(w, "invalid create", 500)
			return
		}
		u.id = "mock-user-id"
		u.username = v.Username
		u.nickname = v.Nickname
		if u.failCreateAfterWrite {
			http.Error(w, "secret-marker", 500)
			return
		}
		u.respond(w)
	case "/api/v3/get-user":
		if r.URL.Query().Get("userId") != u.id || u.id == "" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		u.respond(w)
	case "/api/v3/update-user":
		if u.failUpdate {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			UserID   string `json:"userId"`
			Nickname string `json:"nickname"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || v.UserID != u.id || v.Nickname == "" {
			http.Error(w, "invalid update", 500)
			return
		}
		u.nickname = v.Nickname
		u.respond(w)
	case "/api/v3/delete-users-batch":
		u.deleteAttempts++
		if u.failDeleteOnce && u.deleteAttempts == 1 {
			http.Error(w, "secret-marker", 500)
			return
		}
		var v struct {
			UserIDs []string `json:"userIds"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || len(v.UserIDs) != 1 || v.UserIDs[0] != u.id || u.id == "" {
			http.Error(w, "unowned deletion", 500)
			return
		}
		u.deletes++
		u.id = ""
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	default:
		http.Error(w, "unexpected", 404)
	}
}
func (u *mockUser) respond(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"userId": u.id, "username": u.username, "nickname": u.nickname, "emailVerified": false, "phoneVerified": false}})
}
func mockUserClient(t *testing.T, url string) *authingapi.Client {
	t.Helper()
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "key-marker", AccessKeySecret: "credential-marker", Host: url})
	if e != nil {
		t.Fatal("client setup failed")
	}
	return c
}
func mockUserCredentials(url string) map[string]string {
	return map[string]string{"AUTHING_ACCESS_KEY_ID": "key-marker", "AUTHING_ACCESS_KEY_SECRET": "credential-marker", "AUTHING_HOST": url}
}

func TestMockUserNoPasswordTerraformLifecycle(t *testing.T) {
	u := &mockUser{}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	err := runUserTrace(t.TempDir(), mockUserCredentials(s.URL), "hermesacc-1234567890abcdef")
	if err != nil {
		for _, secret := range []string{"key-marker", "credential-marker", "mock-token", "secret-marker"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatal("diagnostic leaked secret")
			}
		}
		t.Fatal(err)
	}
	u.Lock()
	defer u.Unlock()
	if u.id != "" || u.deletes != 1 || u.nickname != "hermesacc-1234567890abcdef" {
		t.Fatal("lifecycle did not reconcile drift and delete exact ID")
	}
	updates := 0
	for _, p := range u.paths {
		if p == "POST /api/v3/update-user" {
			updates++
		}
	}
	if updates != 2 {
		t.Fatalf("expected drift and reconciliation updates, got %d", updates)
	}
}
func TestMockUserFailedDriftCleansKnownID(t *testing.T) {
	username := "hermesacc-1234567890abcdef"
	u := &mockUser{id: "mock-user-id", username: username, nickname: username, failUpdate: true}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	client := mockUserClient(t, s.URL)
	body, err := client.SendHttpRequestContext(context.Background(), "/api/v3/update-user", "POST", map[string]any{"userId": "mock-user-id", "nickname": username + "-drift"})
	var updated dto.UserSingleRespDto
	if err == nil && json.Unmarshal(body, &updated) == nil && updated.StatusCode == 200 {
		t.Fatal("failed drift was accepted")
	}
	if err := cleanupUser(client, "mock-user-id", username); err != nil {
		t.Fatal("failed to clean up exact owned ID")
	}
	u.Lock()
	defer u.Unlock()
	if u.id != "" || u.deletes != 1 {
		t.Fatal("failed to clean up owned ID")
	}
}
func TestMockUserCreateFailureDoesNotDeleteUnknownID(t *testing.T) {
	u := &mockUser{failCreateAfterWrite: true}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	if runUserTrace(t.TempDir(), mockUserCredentials(s.URL), "hermesacc-1234567890abcdef") == nil {
		t.Fatal("expected failure")
	}
	u.Lock()
	defer u.Unlock()
	if u.id == "" || u.deletes != 0 {
		t.Fatal("deleted unknown ID")
	}
}
func TestMockUserCleanupRefusesDifferentIdentity(t *testing.T) {
	u := &mockUser{id: "mock-user-id", username: "other-user", nickname: "hermesacc-1234567890abcdef"}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	if cleanupUser(mockUserClient(t, s.URL), "mock-user-id", "hermesacc-1234567890abcdef") == nil {
		t.Fatal("accepted foreign identity")
	}
	u.Lock()
	defer u.Unlock()
	if u.deletes != 0 {
		t.Fatal("deleted foreign identity")
	}
}
func TestMockUserCleanupRetriesDelete(t *testing.T) {
	u := &mockUser{id: "mock-user-id", username: "hermesacc-1234567890abcdef", nickname: "hermesacc-1234567890abcdef", failDeleteOnce: true}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	if err := cleanupUser(mockUserClient(t, s.URL), "mock-user-id", "hermesacc-1234567890abcdef"); err != nil {
		t.Fatal(err)
	}
	u.Lock()
	defer u.Unlock()
	if u.id != "" || u.deletes != 1 || u.deleteAttempts != 2 {
		t.Fatal("bounded cleanup failed")
	}
}

func TestMockUserDriftAndExactIDDeletion(t *testing.T) {
	username := "hermesacc-1234567890abcdef"
	u := &mockUser{id: "mock-user-id", username: username, nickname: username}
	s := httptest.NewServer(http.HandlerFunc(u.serve))
	defer s.Close()
	client := mockUserClient(t, s.URL)
	if err := verifyOwnedUser(client, "mock-user-id", username); err != nil {
		t.Fatal(err)
	}
	body, err := client.SendHttpRequestContext(context.Background(), "/api/v3/update-user", "POST", map[string]any{"userId": "mock-user-id", "nickname": username + "-drift"})
	var updated dto.UserSingleRespDto
	if err != nil || json.Unmarshal(body, &updated) != nil || updated.StatusCode != 200 || updated.Data.Nickname != username+"-drift" {
		t.Fatal("mock drift mutation failed")
	}
	if err := verifyOwnedUser(client, "mock-user-id", username); err != nil {
		t.Fatal("drift readback failed")
	}
	if err := cleanupUser(client, "mock-user-id", username); err != nil {
		t.Fatal(err)
	}
	got := client.GetUser(&dto.GetUserDto{UserId: "mock-user-id"})
	if got == nil || got.StatusCode != 404 {
		t.Fatal("exact ID still present after deletion")
	}
}

func TestDestructiveLiveUserTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires -authing-destructive-sandbox and exact confirmation")
	}
	env := map[string]string{
		"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"),
		"AUTHING_ACCESS_KEY_ID":      os.Getenv("AUTHING_ACCESS_KEY_ID"),
		"AUTHING_ACCESS_KEY_SECRET":  os.Getenv("AUTHING_ACCESS_KEY_SECRET"),
		"AUTHING_HOST":               os.Getenv("AUTHING_HOST"),
	}
	if err := destructiveGuard(*destructiveLive, env); err != nil {
		t.Fatal(err)
	}
	username, err := newGroupCode()
	if err != nil {
		t.Fatal("random username generation failed")
	}
	if err := runUserTrace(t.TempDir(), env, username); err != nil {
		t.Fatal(err)
	}
}
