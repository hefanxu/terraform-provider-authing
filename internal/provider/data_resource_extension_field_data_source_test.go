package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestExtensionFieldLookupRegistered(t *testing.T) {
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		meta := datasource.MetadataResponse{}
		factory().Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &meta)
		if meta.TypeName == "authing_data_resource_extension_field" {
			return
		}
	}
	t.Fatal("extension lookup not registered")
}

func extensionFieldLookup(t *testing.T, svc *DataResourceExtensionFieldDataSource, ns, code, key string) datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	schemaResp := datasource.SchemaResponse{}
	svc.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	st := tfsdk.State{Schema: schemaResp.Schema}
	if d := st.Set(ctx, &DataResourceExtensionFieldDataSourceModel{NamespaceCode: types.StringValue(ns), ResourceCode: types.StringValue(code), Key: types.StringValue(key)}); d.HasError() {
		t.Fatal(d)
	}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	svc.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: st.Raw}}, &resp)
	return resp
}

func TestExtensionFieldLookupPaginatesAndMatchesExactKey(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v3/list-dnef" || r.Method != "GET" || r.URL.Query().Get("namespaceCode") != "ns" || r.URL.Query().Get("resourceCode") != "tree" || r.URL.Query().Get("maxSize") != "50" {
			t.Errorf("request %s", r.URL.String())
		}
		switch calls {
		case 1:
			if r.URL.Query().Get("startIndex") != "1" {
				t.Error(r.URL.String())
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"nextStartIndex":2,"truncated":true,"list":[{"key":"other","valueType":"STRING","label":"Other"}]}}`)
		case 2:
			if r.URL.Query().Get("startIndex") != "2" {
				t.Error(r.URL.String())
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"nextStartIndex":-1,"truncated":false,"list":[{"key":"wanted","valueType":"STRING","label":"Wanted","description":"detail"}]}}`)
		default:
			t.Error("extra page")
		}
	})
	out := extensionFieldLookup(t, &DataResourceExtensionFieldDataSource{client: c}, "ns", "tree", "wanted")
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataResourceExtensionFieldDataSourceModel
	if d := out.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != `["ns","tree","wanted"]` || got.Label.ValueString() != "Wanted" || got.ValueType.ValueString() != "STRING" || got.Description.ValueString() != "detail" || calls != 2 {
		t.Fatalf("lookup: %+v calls=%d", got, calls)
	}
}

func TestExtensionFieldLookupRejectsIncompleteAndConflictingPages(t *testing.T) {
	cases := map[string]string{
		"missing":            "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[]}}",
		"duplicate":          "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[{\"key\":\"wanted\",\"valueType\":\"STRING\",\"label\":\"A\"},{\"key\":\"wanted\",\"valueType\":\"STRING\",\"label\":\"B\"}]}}",
		"unknown_type":       "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[{\"key\":\"wanted\",\"valueType\":\"NUMBER\",\"label\":\"A\"}]}}",
		"unknown_property":   "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[{\"key\":\"wanted\",\"valueType\":\"STRING\",\"label\":\"A\",\"unhandled\":1}]}}",
		"duplicate_property": "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[{\"key\":\"wanted\",\"key\":\"wanted\",\"valueType\":\"STRING\",\"label\":\"A\"}]}}",
		"bad_config":         "{\"statusCode\":200,\"data\":{\"nextStartIndex\":-1,\"truncated\":false,\"list\":[{\"key\":\"wanted\",\"valueType\":\"SELECT\",\"label\":\"A\",\"config\":{\"unknown\":1}}]}}",
		"short_page":         "{\"statusCode\":200,\"data\":{\"nextStartIndex\":2,\"truncated\":true,\"list\":[]}}",
		"failure":            "{\"statusCode\":500,\"message\":\"failed\"}",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, reply) })
			out := extensionFieldLookup(t, &DataResourceExtensionFieldDataSource{client: c}, "ns", "tree", "wanted")
			if !out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
				t.Fatalf("accepted invalid list: %v %v", out.Diagnostics, out.State.Raw)
			}
		})
	}
}
