package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

func existingLifecycleResource(kind string, client *authingapi.Client) (resource.Resource, any, string, string) {
	switch kind {
	case "group":
		return &GroupResource{client: client}, GroupModel{ID: types.StringValue("g"), Code: types.StringValue("g"), Name: types.StringValue("old"), Description: types.StringNull()}, "get-group", "delete-groups-batch"
	case "group-member":
		return &GroupMemberResource{client: client}, GroupMemberModel{ID: types.StringValue("g:u"), GroupCode: types.StringValue("g"), UserId: types.StringValue("u")}, "get-user-groups", "remove-group-members"
	case "namespace":
		return &NamespaceResource{client: client}, NamespaceModel{ID: types.StringValue("n"), Code: types.StringValue("n"), Name: types.StringValue("old"), Description: types.StringNull()}, "get-permission-namespace", "delete-permission-namespace"
	case "role":
		return &RoleResource{client: client}, RoleModel{ID: types.StringValue("r"), Code: types.StringValue("r"), Name: types.StringValue("old"), Namespace: types.StringNull(), Description: types.StringNull()}, "get-role", "delete-roles-batch"
	case "role-assignment":
		return &RoleAssignmentResource{client: client}, RoleAssignmentModel{ID: types.StringValue("r:u"), RoleCode: types.StringValue("r"), Namespace: types.StringNull(), TargetType: types.StringValue("USER"), TargetId: types.StringValue("u")}, "", "revoke-role"
	case "resource":
		return &ResourceResource{client: client}, ResourceModel{ID: types.StringValue("res"), Code: types.StringValue("res"), Namespace: types.StringNull(), Type: types.StringValue("DATA"), Description: types.StringNull()}, "get-resource", "delete-resource"
	case "data-policy":
		return &DataPolicyResource{client: client}, DataPolicyModel{ID: types.StringValue("p"), PolicyName: types.StringValue("old"), Description: types.StringNull()}, "get-data-policy", "delete-data-policy"
	case "post":
		return &PostResource{client: client}, PostModel{ID: types.StringValue("post"), Code: types.StringValue("post"), Name: types.StringValue("old"), Description: types.StringNull()}, "get-post", "remove-post"
	case "webhook":
		return &WebhookResource{client: client}, WebhookModel{ID: types.StringValue("wh"), WebhookId: types.StringValue("wh"), Name: types.StringValue("old"), Url: types.StringValue("https://example.com"), Enabled: types.BoolValue(true), Events: types.ListNull(types.StringType), Secret: types.StringNull()}, "get-webhook", "delete-webhook"
	case "pipeline":
		return &PipelineFunctionResource{client: client}, PipelineFunctionModel{ID: types.StringValue("fn"), FuncId: types.StringValue("fn"), FuncName: types.StringValue("old"), FuncDescription: types.StringNull(), Scene: types.StringNull(), SourceCode: types.StringNull(), IsAsynchronous: types.BoolNull()}, "get-pipeline-function", "delete-pipeline-function"
	}
	panic(kind)
}

func TestExistingResourceReadFailuresPreserveState(t *testing.T) {
	for _, kind := range []string{"group", "group-member", "namespace", "role", "resource", "data-policy", "post", "webhook", "pipeline"} {
		for _, tc := range []struct {
			name, body string
			status     int
			removed    bool
		}{
			{"missing", `{"statusCode":404}`, 404, true},
			{"business-missing", `{"statusCode":404}`, 200, true},
			{"server-error", `{"statusCode":500}`, 500, false},
			{"business-error", `{"statusCode":500}`, 200, false},
			{"malformed", `not-json`, 200, false},
			{"empty-data", `{"statusCode":200,"data":{}}`, 200, false},
			{"transport", ``, 0, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
					_, _, endpoint, _ := existingLifecycleResource(kind, nil)
					if r.URL.Path != "/api/v3/"+endpoint {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					if tc.status == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							conn.Close()
						}
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				})
				svc, model, _, _ := existingLifecycleResource(kind, client)
				st := lifecycleState(t, svc, model)
				out := resource.ReadResponse{State: st}
				svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
				if tc.removed {
					if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
						t.Fatalf("expected absent: %v / %v", out.Diagnostics, out.State.Raw)
					}
					return
				}
				if !out.Diagnostics.HasError() || out.State.Raw.IsNull() || !out.State.Raw.Equal(st.Raw) {
					t.Fatalf("read lost state: %v / %v", out.Diagnostics, out.State.Raw)
				}
			})
		}
	}
}

func TestExistingResourceDeleteFailuresReported(t *testing.T) {
	for _, kind := range []string{"group", "group-member", "namespace", "role", "role-assignment", "resource", "data-policy", "post", "webhook", "pipeline"} {
		for _, tc := range []struct {
			name, body string
			status     int
			success    bool
		}{
			{"success", `{"statusCode":200,"data":{"success":true}}`, 200, true},
			{"missing", `{"statusCode":404}`, 404, true},
			{"business-missing", `{"statusCode":404}`, 200, true},
			{"server-error", `{"statusCode":500}`, 500, false},
			{"business-error", `{"statusCode":500}`, 200, false},
			{"false", `{"statusCode":200,"data":{"success":false}}`, 200, kind == "post" || kind == "webhook" || kind == "pipeline" || kind == "data-policy"},
			{"malformed", `not-json`, 200, false},
			{"transport", ``, 0, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
					_, _, _, endpoint := existingLifecycleResource(kind, nil)
					if r.URL.Path != "/api/v3/"+endpoint {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					if tc.status == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							conn.Close()
						}
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
				})
				svc, model, _, _ := existingLifecycleResource(kind, client)
				st := lifecycleState(t, svc, model)
				out := resource.DeleteResponse{State: st}
				svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
				if out.Diagnostics.HasError() == tc.success {
					t.Fatalf("delete success=%v diagnostics=%v", tc.success, out.Diagnostics)
				}
			})
		}
	}
}

func TestPostReadValidEnvelope(t *testing.T) {
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-post" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"code":"post","name":"new"}}`)
	})
	svc, model, _, _ := existingLifecycleResource("post", client)
	st := lifecycleState(t, svc, model)
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if out.Diagnostics.HasError() || out.State.Raw.IsNull() {
		t.Fatalf("valid post read failed: %v", out.Diagnostics)
	}
	var read PostModel
	if d := out.State.Get(context.Background(), &read); d.HasError() || read.Name.ValueString() != "new" {
		t.Fatalf("post read: %#v %v", read, d)
	}
}

func TestGroupMemberReadRequiresCompleteList(t *testing.T) {
	for _, tc := range []struct {
		name, body      string
		absent, failure bool
	}{
		{"present", `{"statusCode":200,"data":{"totalCount":1,"list":[{"code":"g"}]}}`, false, false},
		{"absent", `{"statusCode":200,"data":{"totalCount":1,"list":[{"code":"other"}]}}`, true, false},
		{"incomplete", `{"statusCode":200,"data":{"totalCount":2,"list":[{"code":"other"}]}}`, false, true},
		{"missing-total", `{"statusCode":200,"data":{"list":[]}}`, false, true},
		{"missing-list", `{"statusCode":200,"data":{"totalCount":0}}`, false, true},
		{"invalid-entry", `{"statusCode":200,"data":{"totalCount":1,"list":[{}]}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			svc, model, _, _ := existingLifecycleResource("group-member", client)
			st := lifecycleState(t, svc, model)
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if out.Diagnostics.HasError() != tc.failure || out.State.Raw.IsNull() != tc.absent {
				t.Fatalf("read result: %v / %v", out.Diagnostics, out.State.Raw)
			}
		})
	}
}
