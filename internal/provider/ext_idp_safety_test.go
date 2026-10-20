package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestExtIdpCorruptStateFailsBeforeLookup(t *testing.T) {
	for _, corrupt := range []string{"empty", "different", "unknown"} {
		t.Run(corrupt, func(t *testing.T) {
			svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("corrupt identity reached API: %s", r.URL)
				fmt.Fprint(w, `{"statusCode":404}`)
			})}
			model := extIdpModel("", "Original")
			switch corrupt {
			case "empty":
				model.ExtIdpId = types.StringValue("")
			case "different":
				model.ID = types.StringValue("foreign")
			case "unknown":
				model.ExtIdpId = types.StringUnknown()
			}
			st := extIdpState(t, svc, model)
			read := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &read)
			if !read.Diagnostics.HasError() || !read.State.Raw.Equal(st.Raw) {
				t.Fatalf("corrupt read was accepted: %v", read.Diagnostics)
			}
			deleted := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &deleted)
			if !deleted.Diagnostics.HasError() {
				t.Fatal("corrupt delete was accepted")
			}
		})
	}
}

func TestExtIdpDeleteRefusesUnmanagedConnections(t *testing.T) {
	for _, connections := range []string{`null`, `[{"id":"foreign-connection"}]`, `{}`} {
		t.Run(connections, func(t *testing.T) {
			svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/get-ext-idp" {
					t.Errorf("unsafe delete: %s", r.URL)
				}
				fmt.Fprintf(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Original","type":"oidc","tenantId":"","connections":%s}}`, connections)
			})}

			st := extIdpState(t, svc, extIdpModel("", "Original"))
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if !out.Diagnostics.HasError() {
				t.Fatal("unsafe connections accepted")
			}
		})
	}
}

func TestExtIdpDeleteRequiresAbsenceReadback(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/delete-ext-idp" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Original","type":"oidc","tenantId":"","connections":[]}}`)
	})}
	st := extIdpState(t, svc, extIdpModel("", "Original"))
	out := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("acknowledged delete without absence was accepted")
	}
}

func TestExtIdpReadRejectsMissingName(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","type":"oidc","tenantId":""}}`)
	})}
	st := extIdpState(t, svc, extIdpModel("", "Original"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatal("incomplete IdP response adopted")
	}
}

func TestExtIdpFailedLookupNeverAdoptsOrMutates(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{"statusCode":403}`, `{"statusCode":500}`, `{"statusCode":200,"data":{}}`, `{"statusCode":200,"data":{"id":"foreign","name":"Remote","type":"oidc"}}`} {
		t.Run(body, func(t *testing.T) {
			svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/get-ext-idp" {
					t.Errorf("write after failed lookup: %s", r.URL)
				}
				fmt.Fprint(w, body)
			})}
			st := extIdpState(t, svc, extIdpModel("", "Original"))
			read := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &read)
			if !read.Diagnostics.HasError() || !read.State.Raw.Equal(st.Raw) {
				t.Fatal("invalid lookup adopted")
			}
			plan := tfsdk.Plan{Schema: st.Schema}
			if d := plan.Set(context.Background(), extIdpModel("", "Changed")); d.HasError() {
				t.Fatal(d)
			}
			update := resource.UpdateResponse{State: st}
			svc.Update(context.Background(), resource.UpdateRequest{Plan: plan, State: st}, &update)
			if !update.Diagnostics.HasError() || !update.State.Raw.Equal(st.Raw) {
				t.Fatal("invalid update adopted")
			}
			deleted := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &deleted)
			if !deleted.Diagnostics.HasError() {
				t.Fatal("invalid delete adopted")
			}
		})
	}
}

func TestExtIdpExplicitNotFoundRemovesStateWithoutDelete(t *testing.T) {
	svc := &ExtIdpResource{client: lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("delete attempted for absent object")
		}
		fmt.Fprint(w, `{"statusCode":404}`)
	})}
	st := extIdpState(t, svc, extIdpModel("", "Original"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
		t.Fatal("404 not removed")
	}
	deleted := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
}
