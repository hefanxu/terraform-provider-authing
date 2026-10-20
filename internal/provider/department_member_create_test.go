package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDepartmentMemberCreateRejectsUnsuccessfulResult(t *testing.T) {
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/add-department-members" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":false}}`)
	})
	svc := &DepartmentMemberResource{client: client}
	st := lifecycleState(t, svc, DepartmentMemberModel{ID: types.StringUnknown(), OrganizationCode: types.StringValue("org"), DepartmentId: types.StringValue("dept"), UserId: types.StringValue("user")})
	plan := tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}
	out := resource.CreateResponse{State: tfsdk.State{Schema: st.Schema}}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("false success persisted membership")
	}
}
