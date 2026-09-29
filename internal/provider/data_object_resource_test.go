package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

func objectTestClient(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *authingapi.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "objects-test", AccessKeySecret: "test", Host: server.URL})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func objectPlan(t *testing.T, r resource.Resource, model any) tfsdk.Plan {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	p := tfsdk.Plan{Schema: s.Schema}
	if d := p.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
	return p
}
func objectState(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()
	s := resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	st := tfsdk.State{Schema: s.Schema}
	if d := st.Set(context.Background(), model); d.HasError() {
		t.Fatal(d)
	}
	return st
}
func TestDataObjectCreateAndRead(t *testing.T) {
	calls := []string{}
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/metadata/create-model":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			for k, v := range map[string]any{"name": "Inventory", "description": "Stock", "type": "custom", "parentKey": "root", "enable": true} {
				if body[k] != v {
					t.Errorf("create %s: %v", k, body[k])
				}
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"model-1","name":"Inventory","description":"Stock","type":"custom","parentKey":"root","enable":true,"dataType":"list"}}`)
		case "/api/v3/metadata/get-model":
			if r.URL.Query().Get("id") != "model-1" {
				t.Errorf("get id: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"model-1","name":"Inventory Updated","description":"Stock","type":"custom","parentKey":"root","enable":false,"dataType":"list"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &DataObjectResource{client: c}
	p := objectPlan(t, svc, &DataObjectModel{ID: types.StringUnknown(), Name: types.StringValue("Inventory"), Description: types.StringValue("Stock"), Type: types.StringValue("custom"), ParentKey: types.StringValue("root"), Enable: types.BoolValue(true), DataType: types.StringValue("list")})
	out := resource.CreateResponse{State: objectState(t, svc, &DataObjectModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: p}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataObjectModel
	if d := out.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if got.ID.ValueString() != "model-1" {
		t.Fatalf("unstable id: %v", got.ID)
	}
	read := resource.ReadResponse{State: out.State}
	svc.Read(context.Background(), resource.ReadRequest{State: out.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	if d := read.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if got.Name.ValueString() != "Inventory Updated" || got.Enable.ValueBool() {
		t.Fatalf("drift not reflected: %+v", got)
	}
	if len(calls) != 2 {
		t.Fatal(calls)
	}
}
func TestDataObjectReadFailureDoesNotDiscardState(t *testing.T) {
	for _, status := range []int{500, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"statusCode":%d,"message":"failure"}`, status)
			})
			svc := &DataObjectResource{client: c}
			st := objectState(t, svc, &DataObjectModel{ID: types.StringValue("model-1"), Name: types.StringValue("Stock"), Description: types.StringValue(""), Type: types.StringValue("custom"), ParentKey: types.StringValue(""), Enable: types.BoolValue(true), DataType: types.StringValue("list")})
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if status == 404 && !out.State.Raw.IsNull() {
				t.Fatal("404 should remove state")
			}
			if status == 500 && (!out.Diagnostics.HasError() || out.State.Raw.IsNull()) {
				t.Fatal("transient failure must preserve state and report error")
			}
		})
	}
}
func TestDataObjectCreateRejectsDisplayFieldThatCannotBeApplied(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"model-1"}}`)
	})
	svc := &DataObjectResource{client: c}
	p := objectPlan(t, svc, &DataObjectModel{
		ID: types.StringUnknown(), Name: types.StringValue("Inventory"),
		Description: types.StringValue("Stock"), Type: types.StringValue("custom"),
		ParentKey: types.StringValue(""), Enable: types.BoolValue(true),
		DataType: types.StringValue("list"), ShowFieldKey: types.StringValue("sku"),
	})
	out := resource.CreateResponse{State: objectState(t, svc, &DataObjectModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: p}, &out)
	if !out.Diagnostics.HasError() || calls != 0 {
		t.Fatalf("show_field_key would be silently ignored on create: diagnostics=%v calls=%d", out.Diagnostics, calls)
	}
}
func TestDataObjectUpdateAndDeleteErrors(t *testing.T) {
	failDelete := false
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/metadata/get-model":
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"model-1","fieldOrder":"sku","config":{"layout":"compact"}}}`)
		case "/api/v3/metadata/update-model":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["id"] != "model-1" || b["showFieldKey"] != "sku" || b["fieldOrder"] != "sku" || b["config"] == nil {
				t.Errorf("incomplete update body: %v", b)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"model-1","name":"Changed","description":"Stock","type":"custom","parentKey":"root","enable":true,"dataType":"list"}}`)
		case "/api/v3/metadata/remove-model":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["id"] != "model-1" {
				t.Error(b)
			}
			if failDelete {
				fmt.Fprint(w, `{"statusCode":500,"message":"blocked"}`)
			} else {
				fmt.Fprint(w, `{"statusCode":200}`)
			}
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &DataObjectResource{client: c}
	st := objectState(t, svc, &DataObjectModel{ID: types.StringValue("model-1"), Name: types.StringValue("Stock"), Description: types.StringValue("Stock"), Type: types.StringValue("custom"), ParentKey: types.StringValue("root"), Enable: types.BoolValue(true), DataType: types.StringValue("list")})
	p := objectPlan(t, svc, &DataObjectModel{ID: types.StringValue("model-1"), Name: types.StringValue("Changed"), Description: types.StringValue("Stock"), Type: types.StringValue("custom"), ParentKey: types.StringValue("root"), Enable: types.BoolValue(true), DataType: types.StringValue("list"), ShowFieldKey: types.StringValue("sku")})
	u := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: p, State: st}, &u)
	if u.Diagnostics.HasError() {
		t.Fatal(u.Diagnostics)
	}
	failDelete = true
	d := resource.DeleteResponse{State: u.State}
	svc.Delete(context.Background(), resource.DeleteRequest{State: u.State}, &d)
	if !d.Diagnostics.HasError() {
		t.Fatal("expected delete failure")
	}
	failDelete = false
	d = resource.DeleteResponse{State: u.State}
	svc.Delete(context.Background(), resource.DeleteRequest{State: u.State}, &d)
	if d.Diagnostics.HasError() {
		t.Fatal(d.Diagnostics)
	}
}
func TestDataObjectFieldLifecycle(t *testing.T) {
	name := "Stock keeping unit"
	missing := false
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/metadata/create-field":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			for _, k := range []string{"modelId", "name", "key", "type", "show", "editable", "help", "default", "require", "unique", "maxLength", "max", "min", "regexp", "format", "dropDown", "fuzzySearch", "forLogin", "relationType", "relationMultiple", "relationShowKey", "relationOptionalRange", "userVisible", "accept", "size", "userEditable", "showTenant", "cooperatorEditable"} {
				if _, ok := b[k]; !ok {
					t.Errorf("missing required %s", k)
				}
			}
			if b["modelId"] != "model-1" || b["key"] != "sku" || b["type"] != "Text" {
				t.Error(b)
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"field-1","modelId":"model-1","key":"sku","name":%q,"type":1,"show":true,"editable":true}}`, name)
		case "/api/v3/metadata/list-field":
			if r.URL.Query().Get("modelId") != "model-1" {
				t.Error(r.URL.RawQuery)
			}
			if missing {
				fmt.Fprint(w, `{"statusCode":200,"data":[]}`)
			} else {
				fmt.Fprintf(w, `{"statusCode":200,"data":[{"id":"field-1","modelId":"model-1","key":"sku","name":%q,"type":1,"show":true,"editable":true}]}`, name)
			}
		case "/api/v3/metadata/update-field":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["id"] != "field-1" || b["name"] != "SKU" || b["modelId"] != "model-1" {
				t.Error(b)
			}
			name = "SKU"
			fmt.Fprint(w, `{"statusCode":200,"data":{"id":"field-1","modelId":"model-1","key":"sku","name":"SKU","type":1}}`)
		case "/api/v3/metadata/remove-field":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["id"] != "field-1" || b["modelId"] != "model-1" {
				t.Error(b)
			}
			fmt.Fprint(w, `{"statusCode":200}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	svc := &DataObjectFieldResource{client: c}
	base := DataObjectFieldModel{ID: types.StringUnknown(), ModelID: types.StringValue("model-1"), Key: types.StringValue("sku"), Name: types.StringValue(name), Type: types.StringValue("Text"), Show: types.BoolValue(true), Editable: types.BoolValue(true)}
	p := objectPlan(t, svc, &base)
	out := resource.CreateResponse{State: objectState(t, svc, &DataObjectFieldModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: p}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataObjectFieldModel
	out.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "field-1" {
		t.Fatal(got)
	}
	name = "SKU" // An out-of-band rename must be visible on refresh.
	rd := resource.ReadResponse{State: out.State}
	svc.Read(context.Background(), resource.ReadRequest{State: out.State}, &rd)
	if rd.Diagnostics.HasError() {
		t.Fatal(rd.Diagnostics)
	}
	rd.State.Get(context.Background(), &got)
	if got.Name.ValueString() != "SKU" {
		t.Fatal(got)
	}
	del := resource.DeleteResponse{State: rd.State}
	svc.Delete(context.Background(), resource.DeleteRequest{State: rd.State}, &del)
	if del.Diagnostics.HasError() {
		t.Fatal(del.Diagnostics)
	}
	missing = true
	rd = resource.ReadResponse{State: rd.State}
	svc.Read(context.Background(), resource.ReadRequest{State: rd.State}, &rd)
	if rd.Diagnostics.HasError() || !rd.State.Raw.IsNull() {
		t.Fatalf("missing field: %v %v", rd.Diagnostics, rd.State.Raw)
	}
}
func TestDataObjectFieldListErrorPreservesState(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"statusCode":503,"message":"retry"}`) })
	svc := &DataObjectFieldResource{client: c}
	st := objectState(t, svc, &DataObjectFieldModel{ID: types.StringValue("field-1"), ModelID: types.StringValue("model-1"), Key: types.StringValue("sku"), Name: types.StringValue("SKU"), Type: types.StringValue("Text"), Show: types.BoolValue(true), Editable: types.BoolValue(true)})
	rd := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &rd)
	if !rd.Diagnostics.HasError() || rd.State.Raw.IsNull() {
		t.Fatal("list error removed state")
	}
}
func TestDataObjectFieldUpdateRefusesDestructiveFullUpdate(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"field-1"}}`)
	})
	svc := &DataObjectFieldResource{client: c}
	p := DataObjectFieldModel{ID: types.StringValue("field-1"), ModelID: types.StringValue("model-1"), Key: types.StringValue("sku"), Name: types.StringValue("New"), Type: types.StringValue("Text"), Show: types.BoolValue(true), Editable: types.BoolValue(true)}
	st := objectState(t, svc, &p)
	out := resource.UpdateResponse{State: st}
	svc.Update(context.Background(), resource.UpdateRequest{State: st, Plan: objectPlan(t, svc, &p)}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("unsafe field update should fail instead of resetting unmanaged attributes")
	}
}
func TestDataObjectFieldRejectsUnsupportedType(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"field-1","modelId":"model-1","key":"sku"}}`)
	})
	svc := &DataObjectFieldResource{client: c}
	p := DataObjectFieldModel{ID: types.StringUnknown(), ModelID: types.StringValue("model-1"), Key: types.StringValue("sku"), Name: types.StringValue("SKU"), Type: types.StringValue("Relation"), Show: types.BoolValue(true), Editable: types.BoolValue(true)}
	out := resource.CreateResponse{State: objectState(t, svc, &DataObjectFieldModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, &p)}, &out)
	if !out.Diagnostics.HasError() || calls != 0 {
		t.Fatalf("unsupported type should be rejected before API call: diagnostics=%v calls=%d", out.Diagnostics, calls)
	}
}
func TestDataObjectFieldListWithoutDataIsNotMissing(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"statusCode":200}`) })
	svc := &DataObjectFieldResource{client: c}
	st := objectState(t, svc, &DataObjectFieldModel{ID: types.StringValue("field-1"), ModelID: types.StringValue("model-1"), Key: types.StringValue("sku"), Name: types.StringValue("SKU"), Type: types.StringValue("Text"), Show: types.BoolValue(true), Editable: types.BoolValue(true)})
	rd := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &rd)
	if !rd.Diagnostics.HasError() || rd.State.Raw.IsNull() {
		t.Fatal("malformed list response discarded state")
	}
}
func TestDataObjectImports(t *testing.T) {
	ctx := context.Background()
	model := &DataObjectResource{}
	st := objectState(t, model, &DataObjectModel{})
	out := resource.ImportStateResponse{State: st}
	model.ImportState(ctx, resource.ImportStateRequest{ID: "model-1"}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got DataObjectModel
	out.State.Get(ctx, &got)
	if got.ID.ValueString() != "model-1" {
		t.Fatal(got)
	}
	field := &DataObjectFieldResource{}
	st = objectState(t, field, &DataObjectFieldModel{})
	imported := resource.ImportStateResponse{State: st}
	field.ImportState(ctx, resource.ImportStateRequest{ID: "model-1:field-1"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	var f DataObjectFieldModel
	imported.State.Get(ctx, &f)
	if f.ModelID.ValueString() != "model-1" || f.ID.ValueString() != "field-1" {
		t.Fatal(f)
	}
	invalid := resource.ImportStateResponse{State: st}
	field.ImportState(ctx, resource.ImportStateRequest{ID: "field-1"}, &invalid)
	if !invalid.Diagnostics.HasError() {
		t.Fatal("expected malformed import ID error")
	}
}
func TestObjectResourcesRegistered(t *testing.T) {
	p := &AuthingProvider{}
	found := map[string]bool{}
	for _, f := range p.Resources(context.Background()) {
		md := resource.MetadataResponse{}
		f().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "authing"}, &md)
		found[md.TypeName] = true
	}
	for _, n := range []string{"authing_data_object", "authing_data_object_field"} {
		if !found[n] {
			t.Error(n)
		}
	}
}

var _ = strings.Contains
