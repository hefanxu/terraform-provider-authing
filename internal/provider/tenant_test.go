package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func tenantSet(ids ...string) types.Set {
	v := make([]attr.Value, len(ids))
	for i, id := range ids {
		v[i] = types.StringValue(id)
	}
	return types.SetValueMust(types.StringType, v)
}
func tenantPlan(t *testing.T, r resource.Resource, m any) tfsdk.Plan {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	p := tfsdk.Plan{Schema: s.Schema}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return p
}
func tenantState(t *testing.T, r resource.Resource, m any) tfsdk.State {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	st := tfsdk.State{Schema: s.Schema}
	if d := st.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return st
}
func TestTenantLifecycleAndDrift(t *testing.T) {
	ctx := context.Background()
	name := "Acme"
	apps := []string{"app-a", "app-b"}
	desc := "first"
	source := "source-app"
	exists := true
	calls := []string{}
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/create-tenant", "/api/v3/update-tenant":
			if r.Method != "POST" {
				t.Errorf("method %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if r.URL.Path == "/api/v3/create-tenant" {
				if body["name"] != "Acme" || fmt.Sprint(body["appIds"]) != "[app-a app-b]" || body["sourceAppId"] != "source-app" || body["description"] != "first" {
					t.Errorf("create payload: %v", body)
				}
				if _, ok := body["tenantId"]; ok {
					t.Error("create sent tenantId")
				}
				fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"Acme","appIds":["app-a","app-b"]}}`)
			} else {
				if body["tenantId"] != "tenant-1" || body["name"] != "New" || fmt.Sprint(body["appIds"]) != "[app-c]" || body["description"] != "" || body["sourceAppId"] != "source-app" {
					t.Errorf("update payload: %v", body)
				}
				name = "New"
				apps = []string{"app-c"}
				desc = ""
				fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
			}
		case "/api/v3/get-tenant":
			if r.Method != "GET" || r.URL.Query().Get("tenantId") != "tenant-1" {
				t.Errorf("get request %s %s", r.Method, r.URL)
			}
			if !exists {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			b, _ := json.Marshal(map[string]any{"tenantId": "tenant-1", "name": name, "appIds": apps, "description": desc, "sourceAppId": source, "code": "tenant-code"})
			fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, b)
		case "/api/v3/delete-tenant":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["tenantId"] != "tenant-1" {
				t.Error(b)
			}
			exists = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	svc := &TenantResource{client: c}
	m := TenantModel{ID: types.StringUnknown(), Name: types.StringValue("Acme"), AppIDs: tenantSet("app-a", "app-b"), Description: types.StringValue("first"), SourceAppID: types.StringValue("source-app"), Code: types.StringUnknown()}
	create := resource.CreateResponse{State: tenantState(t, svc, &TenantModel{AppIDs: types.SetNull(types.StringType)})}
	svc.Create(ctx, resource.CreateRequest{Plan: tenantPlan(t, svc, &m)}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	var got TenantModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != "tenant-1" || got.Code.ValueString() != "tenant-code" {
		t.Fatal(got)
	}
	name = "Drift"
	apps = []string{"app-z"}
	desc = "outside"
	read := resource.ReadResponse{State: create.State}
	svc.Read(ctx, resource.ReadRequest{State: create.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(ctx, &got)
	if got.Name.ValueString() != "Drift" || got.Description.ValueString() != "outside" || !got.AppIDs.Equal(tenantSet("app-z")) {
		t.Fatal(got)
	}
	plan := got
	plan.Name = types.StringValue("New")
	plan.AppIDs = tenantSet("app-c")
	plan.Description = types.StringNull()
	update := resource.UpdateResponse{State: read.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: tenantPlan(t, svc, &plan), State: read.State}, &update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	read = resource.ReadResponse{State: update.State}
	svc.Read(ctx, resource.ReadRequest{State: update.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(ctx, &got)
	if got.Name.ValueString() != "New" || !got.Description.IsNull() || !got.AppIDs.Equal(tenantSet("app-c")) {
		t.Fatal(got)
	}
	del := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal(del.Diagnostics)
	}
	read = resource.ReadResponse{State: read.State}
	svc.Read(ctx, resource.ReadRequest{State: read.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Fatalf("delete read: %v %v", read.Diagnostics, read.State.Raw)
	}
	if len(calls) != 8 {
		t.Fatal(calls)
	}
}
func TestTenantReadErrorsAndImport(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, body string
		httpStatus int
		removed    bool
	}{
		{"business 404", `{"statusCode":404}`, 200, true},
		{"http 404", `{"statusCode":404}`, 404, true},
		{"server", `{"statusCode":500}`, 500, false},
		{"business failure", `{"statusCode":403}`, 200, false},
		{"null", `{"statusCode":200,"data":null}`, 200, false},
		{"wrong identity", `{"statusCode":200,"data":{"tenantId":"other","name":"x","appIds":[]}}`, 200, false},
		{"missing apps", `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"x"}}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.httpStatus); fmt.Fprint(w, tc.body) })
			svc := &TenantResource{client: c}
			st := tenantState(t, svc, &TenantModel{ID: types.StringValue("tenant-1"), Name: types.StringValue("old"), AppIDs: tenantSet("a")})
			out := resource.ReadResponse{State: st}
			svc.Read(ctx, resource.ReadRequest{State: st}, &out)
			if tc.removed {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("expected removal: %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("expected preserved state/error: %v", out.Diagnostics)
			}
		})
	}
	svc := &TenantResource{}
	for _, id := range []string{"tenant-1", "", "  "} {
		out := resource.ImportStateResponse{State: tenantState(t, svc, &TenantModel{AppIDs: types.SetNull(types.StringType)})}
		svc.ImportState(ctx, resource.ImportStateRequest{ID: id}, &out)
		if (id == "tenant-1") == out.Diagnostics.HasError() {
			t.Fatalf("import %q: %v", id, out.Diagnostics)
		}
		if id == "tenant-1" {
			var m TenantModel
			out.State.Get(ctx, &m)
			if m.ID.ValueString() != id {
				t.Fatal(m)
			}
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"Imported","appIds":["app-a"],"code":"code"}}`)
			})
			imported := &TenantResource{client: c}
			read := resource.ReadResponse{State: out.State}
			imported.Read(ctx, resource.ReadRequest{State: out.State}, &read)
			if read.Diagnostics.HasError() {
				t.Fatal(read.Diagnostics)
			}
			read.State.Get(ctx, &m)
			if m.Name.ValueString() != "Imported" || !m.AppIDs.Equal(tenantSet("app-a")) || m.Code.ValueString() != "code" {
				t.Fatal(m)
			}
		}
	}
}
func TestTenantWriteFailuresAndIdempotentDelete(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		path, body string
		httpStatus int
	}{
		{"/api/v3/create-tenant", `{"statusCode":200,"data":null}`, 200},
		{"/api/v3/create-tenant", `{"statusCode":403,"message":"denied"}`, 200},
		{"/api/v3/update-tenant", `{"statusCode":200,"data":{"success":false}}`, 200},
		{"/api/v3/delete-tenant", `{"statusCode":200,"data":{"success":false}}`, 200},
		{"/api/v3/delete-tenant", `{"statusCode":500}`, 500},
		{"/api/v3/delete-tenant", `{"statusCode":404}`, 404},
	} {
		t.Run(tc.path+"/"+tc.body, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-tenant" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"old","appIds":[]}}`)
					return
				}
				if r.URL.Path != tc.path {
					t.Errorf("unexpected %s", r.URL.Path)
				}
				w.WriteHeader(tc.httpStatus)
				fmt.Fprint(w, tc.body)
			})
			svc := &TenantResource{client: c}
			m := TenantModel{ID: types.StringValue("tenant-1"), Name: types.StringValue("old"), AppIDs: tenantSet()}
			st := tenantState(t, svc, &m)
			switch tc.path {
			case "/api/v3/create-tenant":
				m.ID = types.StringUnknown()
				out := resource.CreateResponse{State: st}
				svc.Create(ctx, resource.CreateRequest{Plan: tenantPlan(t, svc, &m)}, &out)
				if !out.Diagnostics.HasError() {
					t.Fatal("create should fail")
				}
			case "/api/v3/update-tenant":
				m.Name = types.StringValue("new")
				out := resource.UpdateResponse{State: st}
				svc.Update(ctx, resource.UpdateRequest{Plan: tenantPlan(t, svc, &m), State: st}, &out)
				if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
					t.Fatalf("update must preserve state: %v", out.Diagnostics)
				}
			case "/api/v3/delete-tenant":
				out := resource.DeleteResponse{State: st}
				svc.Delete(ctx, resource.DeleteRequest{State: st}, &out)
				if (tc.httpStatus == 404) == out.Diagnostics.HasError() {
					t.Fatalf("delete 404 only idempotent: %v", out.Diagnostics)
				}
			}
		})
	}
}
func TestTenantEmptyAppSetSerializesArray(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/create-tenant" {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if string(body["appIds"]) != "[]" {
				t.Errorf("empty app_ids must be [], got %s", body["appIds"])
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"Acme","appIds":[]}}`)
	})
	svc := &TenantResource{client: c}
	m := TenantModel{ID: types.StringUnknown(), Name: types.StringValue("Acme"), AppIDs: tenantSet(), Code: types.StringUnknown()}
	out := resource.CreateResponse{State: tenantState(t, svc, &TenantModel{AppIDs: types.SetNull(types.StringType)})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: tenantPlan(t, svc, &m)}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
}
func TestTenantDataSourceErrors(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"statusCode":404}`, 404},
		{`{"statusCode":500}`, 500},
		{`{"statusCode":200,"data":null}`, 200},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			d := &TenantDataSource{client: c}
			s := datasource.SchemaResponse{}
			d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
			st := tfsdk.State{Schema: s.Schema}
			if diag := st.Set(context.Background(), &TenantDataSourceModel{TenantID: types.StringValue("tenant-1"), AppIDs: types.SetNull(types.StringType)}); diag.HasError() {
				t.Fatal(diag)
			}
			out := datasource.ReadResponse{State: tfsdk.State{Schema: s.Schema}}
			d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: s.Schema, Raw: st.Raw}}, &out)
			if !out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
				t.Fatalf("lookup must fail without state: %v", out.Diagnostics)
			}
		})
	}
}
func TestTenantDataSourceLookup(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "tenantId=tenant-1") {
			t.Error(r.URL)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant-1","name":"Acme","appIds":["b","a"],"code":"code"}}`)
	})
	d := &TenantDataSource{client: c}
	s := datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	st := tfsdk.State{Schema: s.Schema}
	if diags := st.Set(context.Background(), &TenantDataSourceModel{TenantID: types.StringValue("tenant-1"), AppIDs: types.SetNull(types.StringType)}); diags.HasError() {
		t.Fatal(diags)
	}
	out := datasource.ReadResponse{State: tfsdk.State{Schema: s.Schema}}
	d.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: s.Schema, Raw: st.Raw}}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got TenantDataSourceModel
	out.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "tenant-1" || got.Name.ValueString() != "Acme" || !got.AppIDs.Equal(tenantSet("a", "b")) {
		t.Fatal(got)
	}
}
