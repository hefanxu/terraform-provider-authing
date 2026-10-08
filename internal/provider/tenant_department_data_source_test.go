package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func tenantDepartmentLookup(t *testing.T, handler func(http.ResponseWriter, *http.Request)) datasource.ReadResponse {
	t.Helper()
	return lookupRead(t, NewTenantDepartmentDataSource(), map[string]string{"tenant_id": "t & 1", "organization_code": "org", "department_id": "child"}, handler)
}

func TestTenantDepartmentLookupScopedRead(t *testing.T) {
	calls := []string{}
	result := tenantDepartmentLookup(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Method != http.MethodGet || r.URL.Query().Get("tenantId") != "t & 1" || r.URL.Query().Get("organizationCode") != "org" || len(r.URL.Query()["tenantId"]) != 1 {
			t.Errorf("unscoped request: %s %s", r.Method, r.URL)
		}
		switch r.URL.Path {
		case "/api/v3/get-organization":
			fmt.Fprint(w, `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`)
		case "/api/v3/get-department":
			if r.URL.Query().Get("departmentId") != "child" || r.URL.Query().Get("departmentIdType") != "department_id" {
				t.Errorf("wrong department request %s", r.URL)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"organizationCode":"org","departmentId":"child","name":"Engineering","parentDepartmentId":"parent","description":"Team","hasChildren":false}}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got TenantDepartmentDataSourceModel
	if d := result.State.Get(context.Background(), &got); d.HasError() {
		t.Fatal(d)
	}
	if len(calls) != 2 || calls[0] != "/api/v3/get-organization" || calls[1] != "/api/v3/get-department" || got.TenantID.ValueString() != "t & 1" || got.DepartmentID.ValueString() != "child" || got.Name.ValueString() != "Engineering" || got.ParentDepartmentID.ValueString() != "parent" || got.Description.ValueString() != "Team" || got.ID.IsNull() {
		t.Fatalf("unexpected state %+v calls %v", got, calls)
	}
}

func TestTenantDepartmentLookupFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, org, dept string
		status          int
		calls           int
	}{
		{"wrong tenant", `{"statusCode":200,"data":{"tenantId":"other","organizationCode":"org","organizationName":"Org"}}`, "", 200, 1},
		{"missing tenant", `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Org"}}`, "", 200, 1},
		{"organization absent", `{"statusCode":404}`, "", 200, 1},
		{"org server error", `{"statusCode":500}`, "", 200, 1},
		{"org malformed", `not json`, "", 200, 1},
		{"department wrong code", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `{"statusCode":200,"data":{"organizationCode":"other","departmentId":"child","name":"Name","parentDepartmentId":"root"}}`, 200, 2},
		{"department wrong ID", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `{"statusCode":200,"data":{"organizationCode":"org","departmentId":"other","name":"Name","parentDepartmentId":"root"}}`, 200, 2},
		{"department missing parent", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `{"statusCode":200,"data":{"organizationCode":"org","departmentId":"child","name":"Name"}}`, 200, 2},
		{"department absent", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `{"statusCode":404}`, 200, 2},
		{"department server error", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `{"statusCode":500}`, 200, 2},
		{"department HTTP failure", `{"statusCode":200,"data":{"tenantId":"t & 1","organizationCode":"org","organizationName":"Org"}}`, `private response`, 502, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			result := tenantDepartmentLookup(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/api/v3/get-organization" {
					fmt.Fprint(w, tc.org)
				} else {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.dept)
				}
			})
			if !result.Diagnostics.HasError() || !result.State.Raw.IsNull() || calls != tc.calls || strings.Contains(fmt.Sprint(result.Diagnostics), "private response") {
				t.Fatalf("unsafe read: diagnostics=%v state=%v calls=%d", result.Diagnostics, result.State.Raw, calls)
			}
		})
	}
}

func TestTenantDepartmentLookupRejectsEmptyIdentityWithoutNetwork(t *testing.T) {
	for _, key := range []string{"tenant_id", "organization_code", "department_id"} {
		t.Run(key, func(t *testing.T) {
			calls := 0
			values := map[string]string{"tenant_id": "tenant", "organization_code": "org", "department_id": "child"}
			values[key] = ""
			result := lookupRead(t, NewTenantDepartmentDataSource(), values, func(http.ResponseWriter, *http.Request) { calls++ })
			if !result.Diagnostics.HasError() || calls != 0 {
				t.Fatalf("diagnostics=%v calls=%d", result.Diagnostics, calls)
			}
		})
	}
}

func TestTenantDepartmentRegisteredReadOnly(t *testing.T) {
	found := false
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		d := factory()
		m := datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		if m.TypeName == "authing_tenant_department" {
			found = true
		}
	}
	if !found {
		t.Fatal("tenant department data source not registered")
	}
	for _, factory := range (&AuthingProvider{}).Resources(context.Background()) {
		r := factory()
		m := resourceMetadata(r)
		if m == "authing_tenant_department" {
			t.Fatal("unsafe tenant department resource registered")
		}
	}
}

func resourceMetadata(r interface {
	Metadata(context.Context, resource.MetadataRequest, *resource.MetadataResponse)
}) string {
	m := resource.MetadataResponse{}
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "authing"}, &m)
	return m.TypeName
}
