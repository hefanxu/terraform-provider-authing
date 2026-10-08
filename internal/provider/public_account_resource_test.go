package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func publicDataState(t *testing.T, d datasource.DataSource) tfsdk.State {
	t.Helper()
	s := datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	return tfsdk.State{Schema: s.Schema}
}
func publicDataConfig(t *testing.T, d datasource.DataSource, m PublicAccountDataModel) tfsdk.Config {
	t.Helper()
	s := datasource.SchemaResponse{}
	d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
	st := tfsdk.State{Schema: s.Schema}
	if diags := st.Set(context.Background(), &m); diags.HasError() {
		t.Fatal(diags)
	}
	return tfsdk.Config{Schema: s.Schema, Raw: st.Raw}
}

func publicPlan() PublicAccountModel {
	return PublicAccountModel{ID: types.StringUnknown(), Username: types.StringValue("shared"), Name: types.StringValue("Shared"), Nickname: types.StringValue("Lobby"), Email: types.StringValue("shared@example.com")}
}

func TestPublicAccountLifecycleAndLookup(t *testing.T) {
	ctx := context.Background()
	present := false
	name := "Shared"
	nickname := "Lobby"
	deletes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-public-account":
			if r.Method != http.MethodGet || r.URL.Query().Get("userId") != "public-1" || r.URL.Query().Get("userIdType") != "user_id" {
				t.Errorf("lookup %s %s", r.Method, r.URL.RawQuery)
			}
			if !present {
				fmt.Fprint(w, `{"statusCode":404}`)
				return
			}
			fmt.Fprintf(w, `{"statusCode":200,"data":{"userId":"public-1","username":"shared","name":%q,"nickname":%q,"email":"shared@example.com"}}`, name, nickname)
		case "/api/v3/create-public-account":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"username": "shared", "name": "Shared", "nickname": "Lobby", "email": "shared@example.com"}) {
				t.Errorf("create body %v", b)
			}
			present = true
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
		case "/api/v3/update-public-account":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"userId": "public-1", "username": "shared", "name": "Updated", "nickname": "Drift", "email": "shared@example.com"}) {
				t.Errorf("update body %v", b)
			}
			name = "Updated"
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
		case "/api/v3/delete-public-accounts-batch":
			deletes++
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if !reflect.DeepEqual(b, map[string]any{"userIds": []any{"public-1"}}) {
				t.Errorf("delete body %v", b)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	svc := &PublicAccountResource{client: c}
	out := resource.CreateResponse{State: objectState(t, svc, &PublicAccountModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, publicPlan())}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	var got PublicAccountModel
	if d := out.State.Get(ctx, &got); d.HasError() || got.ID.ValueString() != "public-1" || got.Name.ValueString() != "Shared" {
		t.Fatalf("create %+v %v", got, d)
	}
	nickname = "Drift"
	rd := resource.ReadResponse{State: out.State}
	svc.Read(ctx, resource.ReadRequest{State: out.State}, &rd)
	if d := rd.State.Get(ctx, &got); d.HasError() || rd.Diagnostics.HasError() || got.Nickname.ValueString() != "Drift" {
		t.Fatalf("drift %+v %v %v", got, d, rd.Diagnostics)
	}
	plan := got
	plan.Name = types.StringValue("Updated")
	upd := resource.UpdateResponse{State: rd.State}
	svc.Update(ctx, resource.UpdateRequest{Plan: objectPlan(t, svc, plan), State: rd.State}, &upd)
	if upd.Diagnostics.HasError() {
		t.Fatal(upd.Diagnostics)
	}
	if d := upd.State.Get(ctx, &got); d.HasError() || got.Name.ValueString() != "Updated" {
		t.Fatalf("update %+v %v", got, d)
	}
	imported := resource.ImportStateResponse{State: objectState(t, svc, &PublicAccountModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: "public-1"}, &imported)
	readImport := resource.ReadResponse{State: imported.State}
	svc.Read(ctx, resource.ReadRequest{State: imported.State}, &readImport)
	if d := readImport.State.Get(ctx, &got); d.HasError() || readImport.Diagnostics.HasError() || got.ID.ValueString() != "public-1" || got.Name.ValueString() != "Updated" {
		t.Fatalf("import %+v %v %v", got, d, readImport.Diagnostics)
	}
	ds := &PublicAccountDataSource{client: c}
	dsout := datasource.ReadResponse{State: publicDataState(t, ds)}
	ds.Read(ctx, datasource.ReadRequest{Config: publicDataConfig(t, ds, PublicAccountDataModel{UserID: types.StringValue("public-1")})}, &dsout)
	var dg PublicAccountDataModel
	if d := dsout.State.Get(ctx, &dg); d.HasError() || dsout.Diagnostics.HasError() || dg.ID.ValueString() != "public-1" || dg.Nickname.ValueString() != "Drift" {
		t.Fatalf("lookup %+v %v %v", dg, d, dsout.Diagnostics)
	}
	del := resource.DeleteResponse{State: upd.State}
	svc.Delete(ctx, resource.DeleteRequest{State: upd.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("delete %v %d", del.Diagnostics, deletes)
	}
	missing := resource.ReadResponse{State: upd.State}
	svc.Read(ctx, resource.ReadRequest{State: upd.State}, &missing)
	if missing.Diagnostics.HasError() || !missing.State.Raw.IsNull() {
		t.Fatalf("missing %v", missing.Diagnostics)
	}
	svc.Delete(ctx, resource.DeleteRequest{State: upd.State}, &del)
	if del.Diagnostics.HasError() || deletes != 1 {
		t.Fatalf("idempotent %v %d", del.Diagnostics, deletes)
	}
}

func TestPublicAccountReadErrorsKeepState(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		missing        bool
	}{
		{"missing", `{"statusCode":404}`, true}, {"server error", `{"statusCode":500}`, false}, {"empty", `{"statusCode":200,"data":null}`, false}, {"wrong identity", `{"statusCode":200,"data":{"userId":"other"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.response) })
			svc := &PublicAccountResource{client: c}
			m := publicPlan()
			m.ID = types.StringValue("public-1")
			state := objectState(t, svc, m)
			out := resource.ReadResponse{State: state}
			svc.Read(context.Background(), resource.ReadRequest{State: state}, &out)
			if tc.missing {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("missing %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() {
				t.Fatalf("lost state %v", out.Diagnostics)
			}
		})
	}
}

func TestPublicAccountDeleteRejectsFalseSuccess(t *testing.T) {
	for _, response := range []string{`{"statusCode":200,"data":{"success":false}}`, `{"statusCode":200}`, `{"statusCode":500}`} {
		c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v3/get-public-account" {
				fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
				return
			}
			fmt.Fprint(w, response)
		})
		svc := &PublicAccountResource{client: c}
		m := publicPlan()
		m.ID = types.StringValue("public-1")
		state := objectState(t, svc, m)
		out := resource.DeleteResponse{State: state}
		svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &out)
		if !out.Diagnostics.HasError() {
			t.Fatalf("accepted %s", response)
		}
	}
}
