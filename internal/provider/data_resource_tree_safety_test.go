package provider

import (
	"context"
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

func TestTreeRejectsUnsafeWritesAndPreservesState(t *testing.T) {
	ctx := context.Background()
	extensions := `[]`
	writes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-data-resource" {
			fmt.Fprintf(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":"N","type":"TREE","struct":{"code":"a","name":"A"},"actions":[],"extendFieldList":%s}}`, extensions)
			return
		}
		writes++
		fmt.Fprint(w, `{"statusCode":200}`)
	})
	svc := &DataResourceResource{client: c}
	m := DataResourceModel{ID: types.StringValue(`["s","r"]`), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("N"), Type: types.StringValue("TREE"), Struct: types.StringValue(`{"code":"a","name":"A"}`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
	st := dataResourceState(t, svc, &m)
	for _, invalid := range []string{`{"code":"a"}`, `{"code":"a","name":"A","children":[{"code":"a","name":"B"}]}`, `{"code":"a","name":"A","extendFieldValue":{"x":"y"}}`} {
		m.Struct = types.StringValue(invalid)
		create := resource.CreateResponse{State: dataResourceState(t, svc, &DataResourceModel{Actions: types.SetNull(types.StringType)})}
		svc.Create(ctx, resource.CreateRequest{Plan: dataResourcePlan(t, svc, &m)}, &create)
		if !create.Diagnostics.HasError() {
			t.Fatal("invalid create accepted", invalid)
		}
		up := resource.UpdateResponse{State: st}
		svc.Update(ctx, resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: st}, &up)
		if !up.Diagnostics.HasError() || up.State.Raw.IsNull() {
			t.Fatal("invalid update accepted", invalid)
		}
	}
	if writes != 0 {
		t.Fatal("invalid structures sent to server", writes)
	}
	m.Struct = types.StringValue(`{"code":"a","name":"A"}`)
	extensions = `[{"key":"secret","valueType":"STRING"}]`
	read := resource.ReadResponse{State: st}
	svc.Read(ctx, resource.ReadRequest{State: st}, &read)
	if !read.Diagnostics.HasError() || read.State.Raw.IsNull() {
		t.Fatal("unmanaged extensions lost on read", read.Diagnostics)
	}
	up := resource.UpdateResponse{State: st}
	svc.Update(ctx, resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: st}, &up)
	if !up.Diagnostics.HasError() || up.State.Raw.IsNull() || !strings.Contains(up.Diagnostics.Errors()[0].Detail(), "extendFieldList") {
		t.Fatal("unmanaged extensions lost on update", up.Diagnostics)
	}
	if writes != 0 {
		t.Fatal("mutated extension-bearing resource")
	}
	extensions = `[ ]`
	read = resource.ReadResponse{State: st}
	svc.Read(ctx, resource.ReadRequest{State: st}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal("whitespace-only empty extension list should be accepted", read.Diagnostics)
	}
}

func TestTreeDataSource(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":"N","type":"TREE","struct":{"name":"A","code":"a"},"actions":[]}}`)
	})
	svc := &DataResourceDataSource{client: c}
	s := datasource.SchemaResponse{}
	svc.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	config := tfsdk.State{Schema: s.Schema}
	if d := config.Set(context.Background(), &DataResourceDataSourceModel{NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), Actions: types.SetNull(types.StringType)}); d.HasError() {
		t.Fatal(d)
	}
	out := datasource.ReadResponse{State: tfsdk.State{Schema: s.Schema}}
	svc.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: s.Schema, Raw: config.Raw}}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataResourceDataSourceModel
	out.State.Get(context.Background(), &got)
	if got.Type.ValueString() != "TREE" || got.Struct.ValueString() != `{"code":"a","name":"A"}` {
		t.Fatal(got)
	}
}
