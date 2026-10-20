package provider

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"net/http"
	"testing"
)

func groupTestModel() GroupModel {
	return GroupModel{ID: types.StringValue("g"), Code: types.StringValue("g"), Name: types.StringValue("Group"), Description: types.StringValue("marker"), Type: types.StringValue("static")}
}
func TestGroupReadRejectsIdentityAndIncompleteFields(t *testing.T) {
	for _, body := range []string{`{"statusCode":200,"data":{"code":"foreign","name":"Group","description":"marker","type":"static"}}`, `{"statusCode":200,"data":{"code":"g","name":"Group","description":"marker"}}`, `{"statusCode":200,"data":{"code":"g","name":"Group","type":"static"}}`, `{"statusCode":403}`, `{"statusCode":422}`, `{"statusCode":500}`, `{"statusCode":404`, `null`} {
		t.Run(body, func(t *testing.T) {
			svc := &GroupResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })}
			st := lifecycleState(t, svc, groupTestModel())
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("uncertain read accepted/removed state: %v", out.Diagnostics)
			}
		})
	}
}
func TestGroupCreateRequiresExactGETReadback(t *testing.T) {
	gets := 0
	svc := &GroupResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-group" {
			gets++
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"foreign","name":"Group","description":"marker","type":"static"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"code":"g","name":"Group","description":"marker","type":"static"}}`)
	})}
	st := lifecycleState(t, svc, groupTestModel())
	plan := tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}
	out := resource.CreateResponse{State: tfsdk.State{Schema: st.Schema}}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &out)
	if !out.Diagnostics.HasError() || gets != 1 || !out.State.Raw.IsNull() {
		t.Fatalf("unverified creation accepted: gets=%d diagnostics=%v", gets, out.Diagnostics)
	}
}
func TestGroupUpdateRejectsGETFieldMismatch(t *testing.T) {
	gets := 0
	svc := &GroupResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-group" {
			gets++
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"g","name":"Old","description":"marker","type":"static"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"code":"g","name":"Group","description":"marker","type":"static"}}`)
	})}
	st := lifecycleState(t, svc, groupTestModel())
	plan := tfsdk.Plan{Schema: st.Schema, Raw: st.Raw}
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: st}, &out)
	if !out.Diagnostics.HasError() || gets != 1 {
		t.Fatalf("unverified update accepted: gets=%d diagnostics=%v", gets, out.Diagnostics)
	}
}
func TestGroupDeleteRequiresExactIdentityAndAbsence(t *testing.T) {
	for _, mode := range []string{"foreign", "mismatched-state", "still-present", "success-false", "confirmed"} {
		t.Run(mode, func(t *testing.T) {
			gets, writes := 0, 0
			svc := &GroupResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-group" {
					gets++
					if mode == "confirmed" && gets > 1 {
						fmt.Fprint(w, `{"statusCode":404}`)
						return
					}
					code := "g"
					if mode == "foreign" {
						code = "foreign"
					}
					fmt.Fprintf(w, `{"statusCode":200,"data":{"code":%q,"name":"Group","description":"marker","type":"static"}}`, code)
					return
				}
				writes++
				fmt.Fprintf(w, `{"statusCode":200,"data":{"success":%t}}`, mode != "success-false")
			})}
			m := groupTestModel()
			if mode == "mismatched-state" {
				m.ID = types.StringValue("foreign")
			}
			st := lifecycleState(t, svc, m)
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if out.Diagnostics.HasError() != (mode != "confirmed") {
				t.Fatalf("wrong result: %v", out.Diagnostics)
			}
			if (mode == "foreign" || mode == "mismatched-state") && writes != 0 {
				t.Fatal("unsafe delete")
			}
			if mode == "confirmed" && (gets != 2 || writes != 1) {
				t.Fatalf("missing postcheck gets=%d writes=%d", gets, writes)
			}
		})
	}
}
