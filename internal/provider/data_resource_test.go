package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func dataResourcePlan(t *testing.T, r resource.Resource, m any) tfsdk.Plan {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	p := tfsdk.Plan{Schema: s.Schema}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return p
}
func dataResourceState(t *testing.T, r resource.Resource, m any) tfsdk.State {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	p := tfsdk.State{Schema: s.Schema}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatal(d)
	}
	return p
}
func TestDataResourceLifecycle(t *testing.T) {
	name := "Inventory"
	description := ""
	structure := `["one","two"]`
	actions := []string{"read", "write"}
	missing := false
	calls := []string{}
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/create-data-resource", "/api/v3/update-data-resource":
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			var b map[string]any
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if b["namespaceCode"] != "space" || b["resourceCode"] != "items" {
				t.Errorf("keys: %v", b)
			}
			if r.URL.Path == "/api/v3/create-data-resource" {
				if b["type"] != "ARRAY" || fmt.Sprint(b["struct"]) != "[one two]" || fmt.Sprint(b["actions"]) != "[read write]" || b["resourceName"] != "Inventory" {
					t.Errorf("create body: %v", b)
				}
				if _, ok := b["description"]; ok {
					t.Error("null optional description sent")
				}
			}
			if r.URL.Path == "/api/v3/update-data-resource" {
				if _, ok := b["type"]; ok {
					t.Error("immutable type sent")
				}
				name = b["resourceName"].(string)
				description = b["description"].(string)
				structure = `["three"]`
				actions = []string{"read"}
				if b["struct"] == nil {
					t.Error("missing struct")
				}
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
		case "/api/v3/get-data-resource":
			if r.Method != "GET" || r.URL.Query().Get("namespaceCode") != "space" || r.URL.Query().Get("resourceCode") != "items" {
				t.Error(r.URL.String())
			}
			if missing {
				fmt.Fprint(w, `{"statusCode":404,"message":"not found"}`)
				return
			}
			a, _ := json.Marshal(map[string]any{"namespaceCode": "space", "resourceCode": "items", "resourceName": name, "description": description, "type": "ARRAY", "struct": json.RawMessage(structure), "actions": actions})
			fmt.Fprintf(w, `{"statusCode":200,"data":%s}`, a)
		case "/api/v3/delete-data-resource":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["namespaceCode"] != "space" || b["resourceCode"] != "items" {
				t.Error(b)
			}
			missing = true
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	svc := &DataResourceResource{client: c}
	ctx := context.Background()
	base := DataResourceModel{ID: types.StringUnknown(), NamespaceCode: types.StringValue("space"), ResourceCode: types.StringValue("items"), ResourceName: types.StringValue(name), Type: types.StringValue("ARRAY"), Struct: types.StringValue(structure), Actions: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("read"), types.StringValue("write")})}
	create := resource.CreateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
	svc.Create(ctx, resource.CreateRequest{Plan: dataResourcePlan(t, svc, &base)}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	var got DataResourceModel
	if d := create.State.Get(ctx, &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != `["space","items"]` {
		t.Fatal(got.ID)
	}
	read := resource.ReadResponse{State: create.State}
	svc.Read(ctx, resource.ReadRequest{State: create.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	updateModel := got
	updateModel.ResourceName = types.StringValue("Updated")
	updateModel.Description = types.StringValue("new")
	updateModel.Struct = types.StringValue(`["three"]`)
	updateModel.Actions = types.SetValueMust(types.StringType, []attr.Value{types.StringValue("read")})
	up := resource.UpdateResponse{State: read.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &updateModel), State: read.State}, &up)
	if up.Diagnostics.HasError() {
		t.Fatal(up.Diagnostics)
	}
	read = resource.ReadResponse{State: up.State}
	svc.Read(ctx, resource.ReadRequest{State: up.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(ctx, &got)
	if got.ResourceName.ValueString() != "Updated" || got.Description.ValueString() != "new" || got.Struct.ValueString() != `["three"]` {
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
		t.Fatalf("missing not removed: %v %v", read.Diagnostics, read.State.Raw)
	}
	if len(calls) != 7 {
		t.Fatal(calls)
	}
}
func TestDataResourceRejectsInvalidStructBeforeNetwork(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	svc := &DataResourceResource{client: c}
	m := DataResourceModel{ID: types.StringUnknown(), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("n"), Type: types.StringValue("ARRAY"), Struct: types.StringValue(`{"wrong":1}`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
	out := resource.CreateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: dataResourcePlan(t, svc, &m)}, &out)
	if !out.Diagnostics.HasError() || calls != 0 {
		t.Fatalf("invalid struct sent: %v calls=%d", out.Diagnostics, calls)
	}
}
func TestDataResourceReadErrorsAndImport(t *testing.T) {
	for _, status := range []int{404, 500, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if status == 200 {
					fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
				} else {
					fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, status)
				}
			})
			svc := &DataResourceResource{client: c}
			st := dataResourceState(t, svc, &DataResourceModel{ID: types.StringValue(`["s","r"]`), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("n"), Type: types.StringValue("STRING"), Struct: types.StringValue(`"v"`), Actions: types.SetValueMust(types.StringType, []attr.Value{})})
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if status == 404 {
				if !out.State.Raw.IsNull() || out.Diagnostics.HasError() {
					t.Fatal(out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("invalid response must preserve state: %v", out.Diagnostics)
			}
		})
	}
	svc := &DataResourceResource{}
	for _, id := range []string{`["s","r"]`, `bad`, `["", "r"]`} {
		out := resource.ImportStateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
		svc.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &out)
		if (id == `["s","r"]`) == out.Diagnostics.HasError() {
			t.Fatalf("import %s: %v", id, out.Diagnostics)
		}
		if id == `["s","r"]` {
			var m DataResourceModel
			out.State.Get(context.Background(), &m)
			if m.NamespaceCode.ValueString() != "s" || m.ResourceCode.ValueString() != "r" {
				t.Fatal(m)
			}
		}
	}
}
func TestDataResourceDataSourceRead(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":"n","type":"STRING","struct":"hello","actions":["read"]}}`)
	})
	svc := &DataResourceDataSource{client: c}
	s := datasource.SchemaResponse{}
	svc.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	cfgState := tfsdk.State{Schema: s.Schema}
	if diags := cfgState.Set(context.Background(), &DataResourceDataSourceModel{NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), Actions: types.SetNull(types.StringType)}); diags.HasError() {
		t.Fatal(diags)
	}
	cfg := tfsdk.Config{Schema: s.Schema, Raw: cfgState.Raw}

	out := datasource.ReadResponse{State: tfsdk.State{Schema: s.Schema}}
	svc.Read(context.Background(), datasource.ReadRequest{Config: cfg}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataResourceDataSourceModel
	out.State.Get(context.Background(), &got)
	if got.Struct.ValueString() != `"hello"` || got.ID.ValueString() != `["s","r"]` {
		t.Fatal(got)
	}
}
