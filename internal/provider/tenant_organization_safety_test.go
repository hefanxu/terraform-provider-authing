package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTenantOrganizationReadErrorsAndMissing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		raw     string
		missing bool
	}{
		{"http404", 404, `{"statusCode":404}`, true},
		{"business404", 200, `{"statusCode":404}`, true},
		{"http500", 500, `{"statusCode":500}`, false},
		{"business500", 200, `{"statusCode":500}`, false},
		{"wrongTenant", 200, `{"statusCode":200,"data":{"tenantId":"other","organizationCode":"code:/one","organizationName":"First"}}`, false},
		{"missingTenant", 200, `{"statusCode":200,"data":{"organizationCode":"code:/one","organizationName":"First"}}`, false},
		{"wrongCode", 200, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"other","organizationName":"First"}}`, false},
		{"missingData", 200, `{"statusCode":200,"data":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.raw) })
			svc := &TenantOrganizationResource{client: c}
			m := tenantOrgModel()
			m.ID = types.StringValue(tenantOrganizationID("tenant:/one", "code:/one"))
			st := objectState(t, svc, m)
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
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
func TestTenantOrganizationCreatePreflightAndReadbackFailures(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after string
		writes        int
	}{
		{"exists", `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First"}}`, ``, 0},
		{"preflight500", `{"statusCode":500}`, ``, 0},
		{"wrongTenant", `{"statusCode":200,"data":{"tenantId":"other","organizationCode":"code:/one","organizationName":"First"}}`, ``, 0},
		{"unconfirmed", `{"statusCode":404}`, `{"statusCode":404}`, 1},
		{"readbackWrongTenant", `{"statusCode":404}`, `{"statusCode":200,"data":{"tenantId":"other","organizationCode":"code:/one","organizationName":"First"}}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, writes := 0, 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-organization" {
					reads++
					if reads == 1 {
						fmt.Fprint(w, tc.before)
					} else {
						fmt.Fprint(w, tc.after)
					}
					return
				}
				writes++
				fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First"}}`)
			})
			svc := &TenantOrganizationResource{client: c}
			ctx := context.Background()
			out := resource.CreateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
			svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, tenantOrgModel())}, &out)
			var m TenantOrganizationModel
			if d := out.State.Get(ctx, &m); d.HasError() || !out.Diagnostics.HasError() || !m.ID.IsNull() || writes != tc.writes {
				t.Fatalf("create %+v %v writes=%d", m, out.Diagnostics, writes)
			}
		})
	}
}
func TestTenantOrganizationDeleteSafety(t *testing.T) {
	for _, tc := range []struct {
		name, pre, mutation, post string
		writes                    int
	}{
		{"wrongTenant", `{"statusCode":200,"data":{"tenantId":"other","organizationCode":"code:/one","organizationName":"First","hasChildren":false}}`, ``, ``, 0},
		{"children", `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First","hasChildren":true}}`, ``, ``, 0},
		{"unknownChildren", `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First"}}`, ``, ``, 0},
		{"get500", `{"statusCode":500}`, ``, ``, 0},
		{"falseSuccess", ``, `{"statusCode":200,"data":{"success":false}}`, ``, 1},
		{"mutation500", ``, `{"statusCode":500}`, ``, 1},
		{"stillPresent", ``, `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First","hasChildren":false}}`, 1},
		{"verify500", ``, `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":500}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, writes := 0, 0
			good := `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First","hasChildren":false}}`
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-organization" {
					reads++
					if reads == 1 && tc.pre != "" {
						fmt.Fprint(w, tc.pre)
					} else if reads > 1 && tc.post != "" {
						fmt.Fprint(w, tc.post)
					} else {
						fmt.Fprint(w, good)
					}
					return
				}
				writes++
				if tc.mutation != "" {
					fmt.Fprint(w, tc.mutation)
				} else {
					fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				}
			})
			svc := &TenantOrganizationResource{client: c}
			m := tenantOrgModel()
			m.ID = types.StringValue(tenantOrganizationID("tenant:/one", "code:/one"))
			st := objectState(t, svc, m)
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if !out.Diagnostics.HasError() || writes != tc.writes {
				t.Fatalf("unsafe delete %v writes=%d", out.Diagnostics, writes)
			}
		})
	}
}
func TestTenantOrganizationImportAndSchema(t *testing.T) {
	svc := NewTenantOrganizationResource()
	s := resource.SchemaResponse{}
	svc.Schema(context.Background(), resource.SchemaRequest{}, &s)
	for _, key := range []string{"tenant_id", "organization_code"} {
		a, ok := s.Schema.Attributes[key].(schema.StringAttribute)
		if !ok || !a.IsRequired() || len(a.PlanModifiers) == 0 {
			t.Errorf("%s not immutable", key)
		}
	}
	id := tenantOrganizationID("tenant:/one", "code:/one")
	ctx := context.Background()
	out := resource.ImportStateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
	svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: id}, &out)
	var m TenantOrganizationModel
	if d := out.State.Get(ctx, &m); d.HasError() || out.Diagnostics.HasError() || m.ID.ValueString() != id || m.TenantID.ValueString() != "tenant:/one" || m.OrganizationCode.ValueString() != "code:/one" {
		t.Fatalf("import %+v %v", m, out.Diagnostics)
	}
	for _, bad := range []string{"bad", tenantOrganizationID("", "code"), tenantOrganizationID("tenant", "")} {
		out := resource.ImportStateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
		svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: bad}, &out)
		if !out.Diagnostics.HasError() {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestTenantOrganizationCreateRejectsMismatchedMutationResponse(t *testing.T) {
	reads := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-organization" {
			reads++
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"another-tenant","organizationCode":"code:/one","organizationName":"First"}}`)
	})
	svc := &TenantOrganizationResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, tenantOrgModel())}, &out)
	if !out.Diagnostics.HasError() || reads != 1 {
		t.Fatalf("accepted mismatched create response: %v reads=%d", out.Diagnostics, reads)
	}
}

func TestTenantOrganizationImportRead(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-organization" || r.URL.Query().Get("tenantId") != "tenant:/one" || r.URL.Query().Get("organizationCode") != "code:/one" {
			t.Errorf("wrong import lookup: %s %s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"Imported","description":"remote","hasChildren":false}}`)
	})
	svc := &TenantOrganizationResource{client: c}
	ctx := context.Background()
	id := tenantOrganizationID("tenant:/one", "code:/one")
	imp := resource.ImportStateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
	svc.ImportState(ctx, resource.ImportStateRequest{ID: id}, &imp)
	if imp.Diagnostics.HasError() {
		t.Fatal(imp.Diagnostics)
	}
	rd := resource.ReadResponse{State: imp.State}
	svc.Read(ctx, resource.ReadRequest{State: imp.State}, &rd)
	var m TenantOrganizationModel
	if d := rd.State.Get(ctx, &m); d.HasError() || rd.Diagnostics.HasError() || m.ID.ValueString() != id || m.OrganizationName.ValueString() != "Imported" || m.Description.ValueString() != "remote" {
		t.Fatalf("imported %+v %v", m, rd.Diagnostics)
	}
}

func TestTenantOrganizationCreateMutationFailure(t *testing.T) {
	for _, raw := range []string{`{"statusCode":500}`, `{"statusCode":200,"data":null}`} {
		t.Run(raw, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-organization" {
					fmt.Fprint(w, `{"statusCode":404}`)
				} else {
					fmt.Fprint(w, raw)
				}
			})
			svc := &TenantOrganizationResource{client: c}
			ctx := context.Background()
			out := resource.CreateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
			svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, tenantOrgModel())}, &out)
			var m TenantOrganizationModel
			if d := out.State.Get(ctx, &m); d.HasError() || !out.Diagnostics.HasError() || !m.ID.IsNull() {
				t.Fatalf("persisted on failed create %+v %v", m, out.Diagnostics)
			}
		})
	}
}

func TestTenantOrganizationCreateReadbackFailureReportsImportID(t *testing.T) {
	reads := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-organization" {
			reads++
			if reads == 1 {
				fmt.Fprint(w, `{"statusCode":404}`)
			} else {
				fmt.Fprint(w, `{"statusCode":500}`)
			}
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"tenant:/one","organizationCode":"code:/one","organizationName":"First"}}`)
	})
	svc := &TenantOrganizationResource{client: c}
	ctx := context.Background()
	out := resource.CreateResponse{State: objectState(t, svc, &TenantOrganizationModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, tenantOrgModel())}, &out)
	if !out.Diagnostics.HasError() || !strings.Contains(fmt.Sprint(out.Diagnostics), tenantOrganizationID("tenant:/one", "code:/one")) {
		t.Fatalf("failed to provide recovery import ID: %v", out.Diagnostics)
	}
}
