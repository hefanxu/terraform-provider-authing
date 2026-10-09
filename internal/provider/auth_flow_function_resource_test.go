package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func flowPlan() AuthFlowFunctionModel {
	return AuthFlowFunctionModel{ID: types.StringUnknown(), FuncName: types.StringValue("first"), Scene: types.StringValue("AUTH_FLOW_FUNCTION"), SourceCode: types.StringValue("async function run() {}"), FuncDescription: types.StringNull(), IsAsynchronous: types.BoolNull(), Timeout: types.Int64Null(), TerminateOnTimeout: types.BoolNull(), Enabled: types.BoolNull()}
}

func TestAuthFlowFunctionLifecycle(t *testing.T) {
	ctx := context.Background()
	present := false
	name := "first"
	code := "async function run() {}"
	deletes := 0
	enabled := false
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/create-auth-flow-function":
			if r.Method != http.MethodPost {
				t.Errorf("create method %s", r.Method)
			}
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"funcName": "first", "scene": "AUTH_FLOW_FUNCTION", "sourceCode": "async function run() {}", "enabled": false}) {
				t.Errorf("create body %v", b)
			}
			present = true
			fmt.Fprint(w, `{"statusCode":200,"data":{"funcId":"flow-1"}}`)
		case "/api/v3/get-auth-flow-function":
			if r.Method != http.MethodGet || r.URL.Query().Get("funcId") != "flow-1" {
				t.Errorf("get %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"funcId":"flow-1","funcName":%q,"funcDescription":"","scene":"AUTH_FLOW_FUNCTION","sourceCode":%q,"isAsynchronous":false,"timeout":3,"terminateOnTimeout":false,"enabled":%t}}`, name, code, enabled)
		case "/api/v3/update-auth-flow-function":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"funcId": "flow-1", "funcName": "updated", "enabled": true}) {
				t.Errorf("update body %v", b)
			}
			name = "updated"
			enabled = true
			fmt.Fprint(w, `{"statusCode":200,"data":{"funcId":"flow-1"}}`)
		case "/api/v3/delete-auth-flow-function":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"funcId": "flow-1"}) {
				t.Errorf("delete body %v", b)
			}
			deletes++
			present = false
			fmt.Fprint(w, `{"statusCode":200,"message":"ok"}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &AuthFlowFunctionResource{client: c}
	plan := flowPlan()
	plan.Enabled = types.BoolValue(false)
	created := resource.CreateResponse{State: objectState(t, svc, &AuthFlowFunctionModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, plan)}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	var got AuthFlowFunctionModel
	if d := created.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != "flow-1" || got.Timeout.ValueInt64() != 3 || got.SourceCode.ValueString() != code {
		t.Fatalf("create %+v %v", got, d)
	}
	name = "drift"
	code = "remote edit"
	rd := resource.ReadResponse{State: created.State}
	svc.Read(ctx, resource.ReadRequest{State: created.State}, &rd)
	if d := rd.State.Get(ctx, &got); d.HasError() || rd.Diagnostics.HasError() || got.FuncName.ValueString() != "drift" || got.SourceCode.ValueString() != "remote edit" {
		t.Fatalf("drift %+v %v %v", got, d, rd.Diagnostics)
	}
	updatedPlan := got
	updatedPlan.FuncName = types.StringValue("updated")
	updatedPlan.Enabled = types.BoolValue(true)
	up := resource.UpdateResponse{State: rd.State}
	svc.Update(ctx, resource.UpdateRequest{State: rd.State, Plan: objectPlan(t, svc, updatedPlan)}, &up)
	if up.Diagnostics.HasError() {
		t.Fatal(up.Diagnostics)
	}
	if d := up.State.Get(ctx, &got); d.HasError() || got.FuncName.ValueString() != "updated" || got.ID.ValueString() != "flow-1" {
		t.Fatalf("update %+v %v", got, d)
	}
	imp := resource.ImportStateResponse{State: objectState(t, svc, &AuthFlowFunctionModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: "flow-1"}, &imp)
	ir := resource.ReadResponse{State: imp.State}
	svc.Read(ctx, resource.ReadRequest{State: imp.State}, &ir)
	if d := ir.State.Get(ctx, &got); d.HasError() || ir.Diagnostics.HasError() || got.ID.ValueString() != "flow-1" || got.SourceCode.ValueString() != "remote edit" {
		t.Fatalf("import %+v %v %v", got, d, ir.Diagnostics)
	}
	del := resource.DeleteResponse{State: up.State}
	svc.Delete(ctx, resource.DeleteRequest{State: up.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("delete %v %d", del.Diagnostics, deletes)
	}
	missing := resource.ReadResponse{State: up.State}
	svc.Read(ctx, resource.ReadRequest{State: up.State}, &missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("missing %v", missing.Diagnostics)
	}
	svc.Delete(ctx, resource.DeleteRequest{State: up.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", del.Diagnostics, deletes)
	}
}

func TestAuthFlowFunctionReadFailuresPreserveState(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		missing        bool
	}{
		{"missing", `{"statusCode":404}`, true},
		{"server", `{"statusCode":500}`, false},
		{"missing data", `{"statusCode":200}`, false},
		{"wrong identity", `{"statusCode":200,"data":{"funcId":"other"}}`, false},
		{"missing source", `{"statusCode":200,"data":{"funcId":"flow-1","funcName":"first","scene":"AUTH_FLOW_FUNCTION"}}`, false},
		{"malformed", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) })
			svc := &AuthFlowFunctionResource{client: c}
			m := flowPlan()
			m.ID = types.StringValue("flow-1")
			state := objectState(t, svc, m)
			out := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &out)
			if tc.missing {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("404 %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("lost state %v", out.Diagnostics)
			}
		})
	}
}

func TestAuthFlowFunctionRejectsFalseWrites(t *testing.T) {
	for _, tc := range []struct{ name, endpoint, response string }{
		{"create missing ID", "/api/v3/create-auth-flow-function", `{"statusCode":200,"data":{}}`},
		{"create readback missing", "/api/v3/create-auth-flow-function", `{"statusCode":200,"data":{"funcId":"flow-1"}}`},
		{"update wrong ID", "/api/v3/update-auth-flow-function", `{"statusCode":200,"data":{"funcId":"other"}}`},
		{"update no effect", "/api/v3/update-auth-flow-function", `{"statusCode":200,"data":{"funcId":"flow-1"}}`},
		{"delete server error", "/api/v3/delete-auth-flow-function", `{"statusCode":500}`},
		{"delete false", "/api/v3/delete-auth-flow-function", `{"statusCode":200,"data":{"success":false}}`},
		{"delete still present", "/api/v3/delete-auth-flow-function", `{"statusCode":200,"message":"ok"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.endpoint {
					fmt.Fprint(w, tc.response)
					return
				}
				if r.URL.Path == "/api/v3/get-auth-flow-function" {
					if tc.name == "create readback missing" {
						fmt.Fprint(w, `{"statusCode":404}`)
						return
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"funcId":"flow-1","funcName":"first","scene":"AUTH_FLOW_FUNCTION","sourceCode":"code","isAsynchronous":false,"timeout":3,"terminateOnTimeout":false,"enabled":false}}`)
					return
				}
				t.Errorf("unexpected call %s", r.URL.Path)
			})
			svc := &AuthFlowFunctionResource{client: c}
			m := flowPlan()
			m.ID = types.StringValue("flow-1")
			m.SourceCode = types.StringValue("code")
			state := objectState(t, svc, m)
			switch {
			case strings.HasPrefix(tc.name, "create"):
				out := resource.CreateResponse{State: objectState(t, svc, &AuthFlowFunctionModel{})}
				svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, flowPlan())}, &out)
				if !out.Diagnostics.HasError() {
					t.Fatalf("false create %v", out.Diagnostics)
				}
				var unchanged AuthFlowFunctionModel
				if d := out.State.Get(context.Background(), &unchanged); d.HasError() || (!unchanged.ID.IsNull() && !unchanged.ID.IsUnknown()) {
					t.Fatalf("create persisted ID %+v %v", unchanged, d)
				}
				if tc.name == "create readback missing" && !strings.Contains(out.Diagnostics.Errors()[0].Detail(), "flow-1") {
					t.Fatalf("missing recovery ID %v", out.Diagnostics)
				}
			case strings.HasPrefix(tc.name, "update"):
				newPlan := m
				newPlan.FuncName = types.StringValue("new")
				out := resource.UpdateResponse{State: state}
				svc.Update(context.Background(), resource.UpdateRequest{State: state, Plan: objectPlan(t, svc, newPlan)}, &out)
				if !out.Diagnostics.HasError() {
					t.Fatalf("false update %v", out.Diagnostics)
				}
			default:
				out := resource.DeleteResponse{State: state}
				svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &out)
				if !out.Diagnostics.HasError() {
					t.Fatalf("false delete %v", out.Diagnostics)
				}
			}
		})
	}
}

func TestAuthFlowFunctionSchemaProtectsSource(t *testing.T) {
	svc := NewAuthFlowFunctionResource()
	s := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &s)
	if !s.Schema.Attributes["source_code"].IsSensitive() {
		t.Fatal("source code not sensitive")
	}
}
