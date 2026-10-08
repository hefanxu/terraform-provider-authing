package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

func lifecycleFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *authingapi.Client {
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
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: "test", AccessKeySecret: "test", Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func lifecycleState(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	st := tfsdk.State{Schema: sr.Schema}
	if d := st.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
	return st
}

func TestOrgDepartmentExtIdpReadFailureDoesNotRemoveState(t *testing.T) {
	for _, kind := range []string{"organization", "department", "ext-idp"} {
		for _, tc := range []struct {
			name, body string
			httpStatus int
			removed    bool
		}{
			{"business-404", `{"statusCode":404,"message":"missing"}`, 200, true},
			{"http-404", `{"statusCode":404,"message":"missing"}`, 404, true},
			{"business-500", `{"statusCode":500,"message":"failure"}`, 200, false},
			{"http-500", `{"statusCode":500,"message":"failure"}`, 500, false},
			{"malformed", `not-json`, 200, false},
			{"empty-data", `{"statusCode":200,"data":{}}`, 200, false},
			{"transport", ``, 0, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/v3/get-"+kind {
						t.Errorf("unexpected path: %s", r.URL.Path)
					}
					if tc.httpStatus == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							conn.Close()
						}
						return
					}
					w.WriteHeader(tc.httpStatus)
					fmt.Fprint(w, tc.body)
				})
				var svc resource.Resource
				var model any
				switch kind {
				case "organization":
					svc = &OrganizationResource{client: client}
					model = OrganizationModel{ID: types.StringValue("org"), OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("old"), Description: types.StringValue("old")}
				case "department":
					svc = &DepartmentResource{client: client}
					model = DepartmentModel{ID: types.StringValue("dept"), OrganizationCode: types.StringValue("org"), DepartmentId: types.StringValue("dept"), Name: types.StringValue("old"), ParentDepartmentId: types.StringValue("root"), Description: types.StringValue("old")}
				case "ext-idp":
					svc = &ExtIdpResource{client: client}
					model = ExtIdpModel{ID: types.StringValue("idp"), ExtIdpId: types.StringValue("idp"), Name: types.StringValue("old"), Type: types.StringValue("oidc"), TenantId: types.StringNull()}
				}
				st := lifecycleState(t, svc, model)
				out := resource.ReadResponse{State: st}
				svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
				if tc.removed {
					if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
						t.Fatalf("missing resource retained: %v / %v", out.Diagnostics, out.State.Raw)
					}
					return
				}
				if !out.Diagnostics.HasError() || out.State.Raw.IsNull() || !out.State.Raw.Equal(st.Raw) {
					t.Fatalf("failure lost state: %v / %v", out.Diagnostics, out.State.Raw)
				}
			})
		}
	}
}

func TestOrgDepartmentExtIdpDeleteFailureReported(t *testing.T) {
	for _, kind := range []string{"organization", "department", "ext-idp", "department-member"} {
		for _, tc := range []struct {
			name, body string
			status     int
			success    bool
		}{
			{"success", `{"statusCode":200,"data":{"success":true}}`, 200, true},
			{"missing", `{"statusCode":404,"message":"missing"}`, 404, true},
			{"failure", `{"statusCode":500,"message":"failed"}`, 500, false},
			{"business-failure", `{"statusCode":500,"message":"failed"}`, 200, false},
			{"false", `{"statusCode":200,"data":{"success":false}}`, 200, false},
			{"malformed", `not-json`, 200, false},
			{"transport", ``, 0, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
					expected := "/api/v3/delete-" + kind
					if kind == "department-member" {
						expected = "/api/v3/remove-department-members"
					}
					if r.URL.Path != expected {
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
				var svc resource.Resource
				var model any
				switch kind {
				case "organization":
					svc = &OrganizationResource{client: client}
					model = OrganizationModel{ID: types.StringValue("org"), OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("old")}
				case "department":
					svc = &DepartmentResource{client: client}
					model = DepartmentModel{ID: types.StringValue("dept"), OrganizationCode: types.StringValue("org"), DepartmentId: types.StringValue("dept"), Name: types.StringValue("old"), ParentDepartmentId: types.StringValue("root")}
				case "ext-idp":
					svc = &ExtIdpResource{client: client}
					model = ExtIdpModel{ID: types.StringValue("idp"), ExtIdpId: types.StringValue("idp"), Name: types.StringValue("old"), Type: types.StringValue("oidc")}
				case "department-member":
					svc = &DepartmentMemberResource{client: client}
					model = DepartmentMemberModel{ID: types.StringValue("org:dept:user"), OrganizationCode: types.StringValue("org"), DepartmentId: types.StringValue("dept"), UserId: types.StringValue("user")}
				}
				st := lifecycleState(t, svc, model)
				out := resource.DeleteResponse{State: st}
				svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
				if out.Diagnostics.HasError() == tc.success {
					t.Fatalf("unexpected delete result: %v", out.Diagnostics)
				}
			})
		}
	}
}
