package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTreeStructValidation(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"root", `{"name":"Root","code":"r"}`, true},
		{"nested", `{"code":"r","name":"Root","children":[{"code":"c","name":"Child","children":[]}]}`, true},
		{"not object", `[]`, false}, {"missing code", `{"name":"N"}`, false}, {"missing name", `{"code":"a"}`, false},
		{"empty code", `{"code":"","name":"N"}`, false}, {"wrong value", `{"code":"a","name":"N","value":1}`, false},
		{"unknown key", `{"code":"a","name":"N","actions":["read"]}`, false},
		{"duplicate key", `{"code":"a","code":"b","name":"N"}`, false},
		{"bad children", `{"code":"a","name":"N","children":[null]}`, false},
		{"duplicate sibling code", `{"code":"r","name":"R","children":[{"code":"x","name":"X"},{"code":"x","name":"Y"}]}`, false},
		{"duplicate sibling name", `{"code":"r","name":"R","children":[{"code":"x","name":"X"},{"code":"y","name":"X"}]}`, false},
		{"cycle code", `{"code":"r","name":"R","children":[{"code":"r","name":"X"}]}`, false},
		{"extension values", `{"code":"r","name":"R","extendFieldValue":{"secret":"x"}}`, false},
		{"empty extension values", `{"code":"r","name":"R","extendFieldValue":{}}`, true},
		{"overlong code", fmt.Sprintf(`{"code":%q,"name":"N"}`, strings.Repeat("x", 51)), false},
		{"overlong name", fmt.Sprintf(`{"code":"a","name":%q}`, strings.Repeat("x", 51)), false},
		{"overlong value", fmt.Sprintf(`{"code":"a","name":"N","value":%q}`, strings.Repeat("x", 1001)), false},
		{"five levels", treeChain(5), true}, {"six levels", treeChain(6), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, canonical, err := dataResourceStruct("TREE", tc.text)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v canonical=%s err=%v", tc.valid, canonical, err)
			}
			if tc.valid && !json.Valid([]byte(canonical)) {
				t.Fatal(canonical)
			}
		})
	}
}
func treeChain(depth int) string {
	node := fmt.Sprintf(`{"code":%q,"name":"Z"}`, fmt.Sprint(depth))
	for i := depth - 1; i > 0; i-- {
		node = fmt.Sprintf(`{"code":%q,"name":"Z","children":[%s]}`, fmt.Sprint(i), node)
	}
	return node
}

func TestTreeLifecycleAndImport(t *testing.T) {
	ctx := context.Background()
	structure := `{"name":"Root","code":"root","children":[{"name":"Child","code":"child"}]}`
	name := "Tree"
	deleted := false
	calls := []string{}
	client := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/create-data-resource", "/api/v3/update-data-resource":
			var b map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			if _, ok := b["extendFieldList"]; ok {
				t.Error("unmanaged extensions sent")
			}
			if r.URL.Path == "/api/v3/create-data-resource" && string(b["type"]) != `"TREE"` {
				t.Error("missing TREE type", b)
			}
			if r.URL.Path == "/api/v3/update-data-resource" {
				if _, ok := b["type"]; ok {
					t.Error("type sent")
				}
			}
			if _, _, err := dataResourceStruct("TREE", string(b["struct"])); err != nil {
				t.Error(err)
			}
			structure = string(b["struct"])
			json.Unmarshal(b["resourceName"], &name)
			fmt.Fprint(w, `{"statusCode":200,"data":{}}`)
		case "/api/v3/get-data-resource":
			if deleted {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":%q,"type":"TREE","struct":%s,"actions":["read"],"extendFieldList":[]}}`, name, structure)
		case "/api/v3/delete-data-resource":
			deleted = true
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &DataResourceResource{client: client}
	m := DataResourceModel{ID: types.StringUnknown(), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue(name), Type: types.StringValue("TREE"), Struct: types.StringValue(structure), Actions: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("read")})}
	create := resource.CreateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
	svc.Create(ctx, resource.CreateRequest{Plan: dataResourcePlan(t, svc, &m)}, &create)
	if create.Diagnostics.HasError() {
		t.Fatal(create.Diagnostics)
	}
	read := resource.ReadResponse{State: create.State}
	svc.Read(ctx, resource.ReadRequest{State: create.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	var got DataResourceModel
	read.State.Get(ctx, &got)
	if got.Struct.ValueString() != m.Struct.ValueString() {
		t.Fatal("format-only drift", got.Struct)
	}
	m = got
	m.ResourceName = types.StringValue("Changed")
	m.Struct = types.StringValue(`{"code":"root","name":"Root","children":[]}`)
	update := resource.UpdateResponse{State: read.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: read.State}, &update)
	if update.Diagnostics.HasError() {
		t.Fatal(update.Diagnostics)
	}
	read = resource.ReadResponse{State: update.State}
	svc.Read(ctx, resource.ReadRequest{State: update.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(ctx, &got)
	if got.Struct.ValueString() != m.Struct.ValueString() || got.ResourceName.ValueString() != "Changed" {
		t.Fatal(got)
	}
	imp := resource.ImportStateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: `["s","r"]`}, &imp)
	if imp.Diagnostics.HasError() {
		t.Fatal(imp.Diagnostics)
	}
	read = resource.ReadResponse{State: imp.State}
	svc.Read(ctx, resource.ReadRequest{State: imp.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	read.State.Get(ctx, &got)
	if got.Type.ValueString() != "TREE" {
		t.Fatal(got)
	}
	_, canonical, err := dataResourceStruct("TREE", m.Struct.ValueString())
	if err != nil || got.Struct.ValueString() != canonical {
		t.Fatal(got, err)
	}
	del := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal(del.Diagnostics)
	}
	read = resource.ReadResponse{State: read.State}
	svc.Read(ctx, resource.ReadRequest{State: read.State}, &read)
	if read.Diagnostics.HasError() || !read.State.Raw.IsNull() {
		t.Fatal(read.Diagnostics)
	}
	if len(calls) != 8 {
		t.Fatal(calls)
	}
}
