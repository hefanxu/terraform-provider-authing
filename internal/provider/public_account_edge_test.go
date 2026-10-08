package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestPublicAccountWritesDoNotPersistUnconfirmedState(t *testing.T) {
	for _, tc := range []struct{ name, create, read string }{
		{"missing ID", `{"statusCode":200,"data":{}}`, `{"statusCode":200,"data":{"userId":"public-1"}}`},
		{"create failure", `{"statusCode":500}`, `{"statusCode":200,"data":{"userId":"public-1"}}`},
		{"readback missing", `{"statusCode":200,"data":{"userId":"public-1"}}`, `{"statusCode":404}`},
		{"readback wrong ID", `{"statusCode":200,"data":{"userId":"public-1"}}`, `{"statusCode":200,"data":{"userId":"other"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/create-public-account" {
					fmt.Fprint(w, tc.create)
				} else {
					fmt.Fprint(w, tc.read)
				}
			})
			svc := &PublicAccountResource{client: c}
			out := resource.CreateResponse{State: objectState(t, svc, &PublicAccountModel{})}
			svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, publicPlan())}, &out)
			var got PublicAccountModel
			if d := out.State.Get(context.Background(), &got); d.HasError() || !out.Diagnostics.HasError() || !got.ID.IsNull() {
				t.Fatalf("false create state %+v %v", got, out.Diagnostics)
			}
		})
	}
}
func TestPublicAccountCreateReadbackFailureReportsRecoverableID(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/create-public-account" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":500}`)
	})
	svc := &PublicAccountResource{client: c}
	out := resource.CreateResponse{State: objectState(t, svc, &PublicAccountModel{})}
	svc.Create(context.Background(), resource.CreateRequest{Plan: objectPlan(t, svc, publicPlan())}, &out)
	if !out.Diagnostics.HasError() || !strings.Contains(out.Diagnostics.Errors()[0].Detail(), "public-1") {
		t.Fatalf("created ID must be recoverable for import: %v", out.Diagnostics)
	}
}

func TestPublicAccountUpdateRejectsMismatchedIdentity(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"other"}}`)
	})
	svc := &PublicAccountResource{client: c}
	m := publicPlan()
	m.ID = types.StringValue("public-1")
	state := objectState(t, svc, m)
	out := resource.UpdateResponse{State: state}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, m), State: state}, &out)
	if !out.Diagnostics.HasError() {
		t.Fatal("mismatched update succeeded")
	}
	var got PublicAccountModel
	_ = out.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "public-1" {
		t.Fatalf("lost state %+v", got)
	}
}
func TestPublicAccountUpdateRequiresReadback(t *testing.T) {
	for _, response := range []string{`{"statusCode":404}`, `{"statusCode":500}`, `{"statusCode":200,"data":{"userId":"other"}}`} {
		c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v3/update-public-account" {
				fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
				return
			}
			fmt.Fprint(w, response)
		})
		svc := &PublicAccountResource{client: c}
		m := publicPlan()
		m.ID = types.StringValue("public-1")
		state := objectState(t, svc, m)
		out := resource.UpdateResponse{State: state}
		svc.Update(context.Background(), resource.UpdateRequest{Plan: objectPlan(t, svc, m), State: state}, &out)
		if !out.Diagnostics.HasError() {
			t.Fatalf("accepted failed readback %s", response)
		}
		var got PublicAccountModel
		_ = out.State.Get(context.Background(), &got)
		if got.ID.ValueString() != "public-1" {
			t.Fatalf("lost state %+v", got)
		}
	}
}
func TestPublicAccountDeleteRequiresConfirmedAbsence(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-public-account" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"userId":"public-1"}}`)
			return
		}
		calls++
		fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
	})
	svc := &PublicAccountResource{client: c}
	m := publicPlan()
	m.ID = types.StringValue("public-1")
	state := objectState(t, svc, m)
	out := resource.DeleteResponse{State: state}
	svc.Delete(context.Background(), resource.DeleteRequest{State: state}, &out)
	if !out.Diagnostics.HasError() || calls != 1 {
		t.Fatalf("unconfirmed delete %v calls %d", out.Diagnostics, calls)
	}
}
func TestPublicAccountLookupMissingAndFailure(t *testing.T) {
	for _, response := range []string{`{"statusCode":404}`, `{"statusCode":500}`, `{"statusCode":200,"data":{"userId":"other"}}`} {
		c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) })
		ds := &PublicAccountDataSource{client: c}
		out := datasource.ReadResponse{State: publicDataState(t, ds)}
		ds.Read(context.Background(), datasource.ReadRequest{Config: publicDataConfig(t, ds, PublicAccountDataModel{UserID: types.StringValue("public-1")})}, &out)
		if !out.Diagnostics.HasError() {
			t.Fatalf("lookup accepted %s", response)
		}
	}
}
