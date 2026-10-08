package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDataResourceExtensionDefinitionsReadButBlockParentUpdate(t *testing.T) {
	for _, fields := range []string{`[{"key":"department","valueType":"STRING","label":"Department"}]`, `[{"key":"department","valueType":"SELECT","label":"Department","config":{"options":[{"value":"one"}]}}]`} {
		t.Run(fields, func(t *testing.T) {
			writes := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/update-data-resource" {
					writes++
					fmt.Fprint(w, `{"statusCode":200}`)
					return
				}
				if r.URL.Path != "/api/v3/get-data-resource" {
					t.Error(r.URL.Path)
					return
				}
				fmt.Fprintf(w, `{"statusCode":200,"data":{"namespaceCode":"ns","resourceCode":"tree","resourceName":"Tree","type":"TREE","struct":{"code":"root","name":"Root"},"actions":[],"extendFieldList":%s}}`, fields)
			})
			svc := &DataResourceResource{client: c}
			m := DataResourceModel{ID: types.StringValue(`["ns","tree"]`), NamespaceCode: types.StringValue("ns"), ResourceCode: types.StringValue("tree"), ResourceName: types.StringValue("Tree"), Type: types.StringValue("TREE"), Struct: types.StringValue(`{"code":"root","name":"Root"}`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
			st := dataResourceState(t, svc, &m)
			read := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &read)
			if read.Diagnostics.HasError() {
				t.Fatal(read.Diagnostics)
			}
			m.ResourceName = types.StringValue("Changed")
			update := resource.UpdateResponse{State: read.State}
			svc.Update(context.Background(), resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: read.State}, &update)
			if !update.Diagnostics.HasError() || writes != 0 {
				t.Fatalf("extension parent update permitted: %v writes=%d", update.Diagnostics, writes)
			}
		})
	}
}
func TestDataResourceRejectsMalformedExtensionsOnRead(t *testing.T) {
	for _, fields := range []string{`[{"key":"field","valueType":"OTHER","label":"label"}]`, `[{"key":"field","valueType":"STRING","label":"label","unexpected":true}]`, `[{"key":"field","valueType":"STRING","label":"label"},{"key":"field","valueType":"STRING","label":"again"}]`, `{"wrong":true}`} {
		t.Run(fields, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"statusCode":200,"data":{"namespaceCode":"ns","resourceCode":"tree","resourceName":"Tree","type":"TREE","struct":{"code":"root","name":"Root"},"actions":[],"extendFieldList":%s}}`, fields)
			})
			svc := &DataResourceResource{client: c}
			m := DataResourceModel{ID: types.StringValue(`["ns","tree"]`), NamespaceCode: types.StringValue("ns"), ResourceCode: types.StringValue("tree"), ResourceName: types.StringValue("Tree"), Type: types.StringValue("TREE"), Struct: types.StringValue(`{"code":"root","name":"Root"}`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
			st := dataResourceState(t, svc, &m)
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("malformed extension accepted: %v", out.Diagnostics)
			}
		})
	}
}
