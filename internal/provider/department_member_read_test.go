package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDepartmentMemberReadChecksRemoteMembership(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		status         int
		missing, fails bool
	}{
		{"present", `{"statusCode":200,"data":{"totalCount":1,"list":[{"userId":"user"}]}}`, 200, false, false},
		{"absent", `{"statusCode":200,"data":{"totalCount":1,"list":[{"userId":"other"}]}}`, 200, true, false},
		{"empty", `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`, 200, true, false},
		{"no-count-empty", `{"statusCode":200,"data":{"list":[]}}`, 200, true, false},
		{"api-failure", `{"statusCode":500,"message":"failed"}`, 200, false, true},
		{"http-failure", `{"statusCode":500,"message":"failed"}`, 500, false, true},
		{"malformed", `not-json`, 200, false, true},
		{"missing-list", `{"statusCode":200,"data":{}}`, 200, false, true},
		{"missing-user-id", `{"statusCode":200,"data":{"totalCount":1,"list":[{}]}}`, 200, false, true},
		{"incomplete-page", `{"statusCode":200,"data":{"totalCount":2,"list":[{"userId":"other"}]}}`, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/list-department-members" || r.URL.Query().Get("organizationCode") != "org" || r.URL.Query().Get("departmentId") != "dept" {
					t.Errorf("unexpected request: %s", r.URL.String())
				}
				if r.URL.Query().Get("page") == "2" && tc.name == "incomplete-page" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":2,"list":[]}}`)
					return
				}
				if r.URL.Query().Get("page") != "1" {
					t.Errorf("unexpected page: %s", r.URL.String())
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			svc := &DepartmentMemberResource{client: client}
			st := lifecycleState(t, svc, DepartmentMemberModel{ID: types.StringValue("org:dept:user"), OrganizationCode: types.StringValue("org"), DepartmentId: types.StringValue("dept"), UserId: types.StringValue("user")})
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if out.Diagnostics.HasError() != tc.fails || out.State.Raw.IsNull() != tc.missing {
				t.Fatalf("read result: diagnostics=%v state=%v", out.Diagnostics, out.State.Raw)
			}
		})
	}
}
