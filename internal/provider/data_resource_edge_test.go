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

func TestDataResourceStructRejectsUnsupportedAndMalformedValues(t *testing.T) {
	for _, test := range []struct{ kind, text string }{
		{"TREE", `{"code":"a","name":"A"}`},
		{"STRING", `["wrong"]`}, {"STRING", `""`},
		{"ARRAY", `{"wrong":1}`}, {"ARRAY", `["a","a"]`}, {"ARRAY", `[1]`}, {"ARRAY", `["x"] false`}, {"ARRAY", `["x"] invalid`},
	} {
		t.Run(test.kind+"/"+test.text, func(t *testing.T) {
			if _, _, err := dataResourceStruct(test.kind, test.text); err == nil {
				t.Fatal("accepted unsafe structure")
			}
		})
	}
}
func TestDataResourceReadDriftAndRejectsUnmanagedTree(t *testing.T) {
	variant := "ARRAY"
	remoteStruct := `["changed"]`
	remoteName := "Changed"
	remoteActions := `["write"]`
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":%q,"type":%q,"struct":%s,"actions":%s}}`, remoteName, variant, remoteStruct, remoteActions)
	})
	svc := &DataResourceResource{client: c}
	st := dataResourceState(t, svc, &DataResourceModel{ID: types.StringValue(`["s","r"]`), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("Original"), Type: types.StringValue("ARRAY"), Struct: types.StringValue(`["original"]`), Actions: types.SetValueMust(types.StringType, []attr.Value{types.StringValue("read")})})
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataResourceModel
	out.State.Get(context.Background(), &got)
	if got.ResourceName.ValueString() != remoteName || got.Struct.ValueString() != remoteStruct || !strings.Contains(got.Actions.String(), "write") {
		t.Fatal(got)
	}
	variant = "TREE"
	remoteStruct = `{"code":"root","name":"Root"}`
	out = resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
		t.Fatal("tree must not be silently imported or discard state")
	}
}
func TestDataResourceUpdateDeleteAndDataSourceErrors(t *testing.T) {
	status := 500
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, status)
	})
	svc := &DataResourceResource{client: c}
	m := DataResourceModel{ID: types.StringValue(`["s","r"]`), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("n"), Type: types.StringValue("STRING"), Struct: types.StringValue(`"v"`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
	st := dataResourceState(t, svc, &m)
	up := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: st}, &up)
	if !up.Diagnostics.HasError() || up.State.Raw.IsNull() {
		t.Fatal("update error must preserve state")
	}
	del := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &del)
	if !del.Diagnostics.HasError() {
		t.Fatal("delete error ignored")
	}
	status = 404
	del = resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal("idempotent delete failed", del.Diagnostics)
	}
	ds := &DataResourceDataSource{client: c}
	schema := datasource.SchemaResponse{}
	ds.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
	cfgSt := tfsdk.State{Schema: schema.Schema}
	if d := cfgSt.Set(context.Background(), &DataResourceDataSourceModel{NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), Actions: types.SetNull(types.StringType)}); d.HasError() {
		t.Fatal(d)
	}
	out := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	ds.Read(context.Background(), datasource.ReadRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: cfgSt.Raw}}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("missing lookup must error")
	}
}
