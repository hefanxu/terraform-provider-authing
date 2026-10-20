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

func TestDataResourceUpdateRefusesRemoteTypeChange(t *testing.T) {
	updates := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-data-resource":
			fmt.Fprint(w, `{"statusCode":200,"data":{"namespaceCode":"s","resourceCode":"r","resourceName":"tree","type":"TREE","struct":{"code":"root","name":"root"},"actions":[]}}`)
		case "/api/v3/update-data-resource":
			updates++
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			t.Error(r.URL.Path)
		}
	})
	svc := &DataResourceResource{client: c}
	m := DataResourceModel{ID: types.StringValue(`["s","r"]`), NamespaceCode: types.StringValue("s"), ResourceCode: types.StringValue("r"), ResourceName: types.StringValue("renamed"), Type: types.StringValue("STRING"), Struct: types.StringValue(`"hello"`), Actions: types.SetValueMust(types.StringType, []attr.Value{})}
	st := dataResourceState(t, svc, &m)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: dataResourcePlan(t, svc, &m), State: st}, &out)
	if !out.Diagnostics.HasError() || updates != 0 {
		t.Fatalf("unsafe update attempted: %v %d", out.Diagnostics, updates)
	}
}
