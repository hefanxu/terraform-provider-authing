package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

func userFixture(t *testing.T, handler http.HandlerFunc) (*UserResource, tfsdk.State, tfsdk.Plan) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "test-id", AccessKeySecret: "test-secret", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	svc := &UserResource{client: client}
	var schema resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	return svc, tfsdk.State{Schema: schema.Schema}, tfsdk.Plan{Schema: schema.Schema}
}

func userModel() UserModel {
	return UserModel{ID: types.StringValue("user-1"), Username: types.StringValue("alice"), Email: types.StringValue("old@example.com"), Phone: types.StringValue("123"), Nickname: types.StringNull(), Password: types.StringNull(), ExternalId: types.StringNull(), Status: types.StringNull(), Gender: types.StringNull(), EmailVerified: types.BoolValue(true), PhoneVerified: types.BoolValue(true)}
}
func setUserModel(t *testing.T, state *tfsdk.State, model UserModel) {
	t.Helper()
	if d := state.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
}
func getUserModel(t *testing.T, state tfsdk.State) UserModel {
	t.Helper()
	var m UserModel
	if d := state.Get(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return m
}

func TestUserReadOnlyExplicit404RemovesState(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		body    string
		missing bool
	}{
		{"HTTP 404", 404, `{"statusCode":404,"message":"missing"}`, true},
		{"business 404", 200, `{"statusCode":404,"message":"missing"}`, true},
		{"HTTP 503", 503, `{"statusCode":503,"message":"unavailable"}`, false},
		{"business 403", 200, `{"statusCode":403,"message":"forbidden"}`, false},
		{"malformed", 200, `not-json`, false},
		{"missing userId", 200, `{"statusCode":200,"data":{"email":"wrong@example.com"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, _ := userFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v3/get-user" || r.URL.Query().Get("userId") != "user-1" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			})
			original := userModel()
			setUserModel(t, &state, original)
			resp := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if tc.missing {
				if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
					t.Fatalf("404 must remove state: %v %#v", resp.Diagnostics, resp.State.Raw)
				}
				return
			}
			if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatalf("read failure erased state or lacked diagnostic: %v %#v", resp.Diagnostics, resp.State.Raw)
			}
			if got := getUserModel(t, resp.State); !reflect.DeepEqual(got, original) {
				t.Fatalf("failure mutated state: got %+v want %+v", got, original)
			}
		})
	}
}

func TestUserUpdateSendsIdentityAndVerificationAndReadsBack(t *testing.T) {
	var payload map[string]any
	gets := 0
	svc, state, plan := userFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/update-user":
			if r.Method != http.MethodPost {
				t.Errorf("update method %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"user-1"}}`)
		case "/api/v3/get-user":
			gets++
			if r.Method != http.MethodGet || r.URL.Query().Get("userId") != "user-1" {
				t.Errorf("get request %s %s", r.Method, r.URL)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"user-1","username":"alice","email":"new@example.com","phone":"456","emailVerified":false,"phoneVerified":false}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	old := userModel()
	setUserModel(t, &state, old)
	next := old
	next.Email = types.StringValue("new@example.com")
	next.Phone = types.StringValue("456")
	next.EmailVerified = types.BoolValue(false)
	next.PhoneVerified = types.BoolValue(false)
	if d := plan.Set(context.Background(), next); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	want := map[string]any{"userId": "user-1", "username": "alice", "email": "new@example.com", "phone": "456", "emailVerified": false, "phoneVerified": false}
	if !reflect.DeepEqual(payload, want) {
		t.Errorf("payload: got %#v want %#v", payload, want)
	}
	if gets != 1 {
		t.Errorf("readback count %d", gets)
	}
	if got := getUserModel(t, resp.State); !reflect.DeepEqual(got, next) {
		t.Errorf("state: got %+v want %+v", got, next)
	}
}

func TestUserUpdateReadbackMismatchPreservesState(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"stale email", `{"statusCode":200,"data":{"userId":"user-1","username":"alice","email":"old@example.com","phone":"123","emailVerified":true,"phoneVerified":true}}`},
		{"readback failed", `{"statusCode":503,"message":"unavailable"}`},
		{"missing id", `{"statusCode":200,"data":{}}`},
		{"different id", `{"statusCode":200,"data":{"userId":"other-user","username":"alice","email":"new@example.com","phone":"123","emailVerified":false,"phoneVerified":true}}`},
		{"verification not applied", `{"statusCode":200,"data":{"userId":"user-1","username":"alice","email":"new@example.com","phone":"123","emailVerified":true,"phoneVerified":true}}`},
		{"missing verification flags", `{"statusCode":200,"data":{"userId":"user-1","username":"alice","email":"new@example.com","phone":"123"}}`},
		{"null verification flags", `{"statusCode":200,"data":{"userId":"user-1","username":"alice","email":"new@example.com","phone":"123","emailVerified":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, state, plan := userFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/update-user":
					fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"user-1"}}`)
				case "/api/v3/get-user":
					fmt.Fprint(w, tc.body)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			})
			old := userModel()
			setUserModel(t, &state, old)
			next := old
			next.Email = types.StringValue("new@example.com")
			next.EmailVerified = types.BoolValue(false)
			if tc.name == "missing verification flags" || tc.name == "null verification flags" {
				next.PhoneVerified = types.BoolNull()
			}
			if d := plan.Set(context.Background(), next); d.HasError() {
				t.Fatal(d)
			}
			resp := resource.UpdateResponse{State: state}
			svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatal("update declared success without confirmed readback")
			}
			if got := getUserModel(t, resp.State); !reflect.DeepEqual(got, old) {
				t.Fatalf("false state: %+v", got)
			}
		})
	}
}
