package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func roleAssignmentModel(kind string) RoleAssignmentModel {
	return RoleAssignmentModel{ID: types.StringValue("role:one:" + kind + ":target/one"), RoleCode: types.StringValue("role:one"), Namespace: types.StringValue("ns:one"), TargetType: types.StringValue(kind), TargetId: types.StringValue("target/one")}
}

func TestRoleAssignmentDirectReadLifecycle(t *testing.T) {
	for _, tc := range []struct{ kind, endpoint, key string }{
		{"USER", "list-role-members", "userId"},
		{"DEPARTMENT", "list-role-departments", "id"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			present := false
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/assign-role":
					present = true
					fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				case "/api/v3/" + tc.endpoint:
					q := r.URL.Query()
					if r.Method != http.MethodGet || q.Get("code") != "role:one" || q.Get("namespace") != "ns:one" || q.Get("page") != "1" || q.Get("limit") != "50" {
						t.Errorf("incorrect direct-list request: %s %s", r.Method, r.URL.RawQuery)
					}
					if present {
						fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":2,"list":[{"%s":"other"},{"%s":"target/one"}]}}`, tc.key, tc.key)
					} else {
						fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":0,"list":[]}}`)
					}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			})
			svc := &RoleAssignmentResource{client: c}
			m := roleAssignmentModel(tc.kind)
			m.ID = types.StringUnknown()
			plan := objectPlan(t, svc, m)
			create := resource.CreateResponse{State: objectState(t, svc, &RoleAssignmentModel{})}
			svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &create)
			if create.Diagnostics.HasError() {
				t.Fatal(create.Diagnostics)
			}
			read := resource.ReadResponse{State: create.State}
			svc.Read(context.Background(), resource.ReadRequest{State: create.State}, &read)
			if read.Diagnostics.HasError() || read.State.Raw.IsNull() || !read.State.Raw.Equal(create.State.Raw) {
				t.Fatalf("refresh: %v %v", read.Diagnostics, read.State.Raw)
			}
			present = false
			drift := resource.ReadResponse{State: read.State}
			svc.Read(context.Background(), resource.ReadRequest{State: read.State}, &drift)
			if drift.Diagnostics.HasError() || !drift.State.Raw.IsNull() {
				t.Fatalf("drift: %v %v", drift.Diagnostics, drift.State.Raw)
			}
		})
	}
}

func TestRoleAssignmentReadPaginationAndExactIdentity(t *testing.T) {
	for _, kind := range []string{"USER", "DEPARTMENT"} {
		t.Run(kind, func(t *testing.T) {
			key, endpoint := "userId", "list-role-members"
			if kind == "DEPARTMENT" {
				key, endpoint = "id", "list-role-departments"
			}
			calls := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/"+endpoint {
					t.Errorf("endpoint: %s", r.URL.Path)
				}
				calls++
				if r.URL.Query().Get("page") != fmt.Sprint(calls) {
					t.Errorf("page: %s", r.URL.RawQuery)
				}
				if calls == 1 {
					fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":51,"list":[%s]}}`, strings.TrimSuffix(strings.Repeat(fmt.Sprintf(`{"%s":"other"},`, key), 50), ","))
				} else {
					fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":51,"list":[{"%s":"target/one"}]}}`, key)
				}
			})
			svc := &RoleAssignmentResource{client: c}
			st := objectState(t, svc, roleAssignmentModel(kind))
			read := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &read)
			if read.Diagnostics.HasError() || read.State.Raw.IsNull() || calls != 2 {
				t.Fatalf("pagination: calls=%d diag=%v state=%v", calls, read.Diagnostics, read.State.Raw)
			}
		})
	}
}

func TestRoleAssignmentReadDoesNotCountInheritedUserRole(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/list-role-members" {
			t.Errorf("must query direct grants, not effective/inherited roles: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[{"userId":"target/one-extra"}]}}`)
	})
	svc := &RoleAssignmentResource{client: c}
	st := objectState(t, svc, roleAssignmentModel("USER"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
		t.Fatalf("unrelated direct member must not retain an inherited grant: %v", out.Diagnostics)
	}
}

func TestRoleAssignmentReadDefaultNamespaceIsOmitted(t *testing.T) {
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["namespace"]; ok || r.URL.Query().Get("code") != "role:one" {
			t.Errorf("wrong default namespace query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":1,"list":[{"id":"target/one"}]}}`)
	})
	svc := &RoleAssignmentResource{client: c}
	m := roleAssignmentModel("DEPARTMENT")
	m.Namespace = types.StringNull()
	st := objectState(t, svc, m)
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("default namespace should retain exact stored identity: %v", out.Diagnostics)
	}
}

func TestRoleAssignmentReadLaterPageNotFoundPreservesState(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":51,"list":[%s]}}`, strings.TrimSuffix(strings.Repeat(`{"userId":"other"},`, 50), ","))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"statusCode":404}`)
	})
	svc := &RoleAssignmentResource{client: c}
	st := objectState(t, svc, roleAssignmentModel("USER"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if calls != 2 || !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("lost state after pagination failure: calls=%d diag=%v", calls, out.Diagnostics)
	}
}

func TestRoleAssignmentReadUncertainResponseKeepsState(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		removed    bool
	}{
		{"missing-role", `{"statusCode":404}`, 404, true},
		{"business-missing-role", `{"statusCode":404}`, 200, true},
		{"server", `{"statusCode":500}`, 500, false},
		{"business-error", `{"statusCode":500}`, 200, false},
		{"malformed", `bad-json`, 200, false},
		{"missing-list", `{"statusCode":200,"data":{"totalCount":0}}`, 200, false},
		{"missing-id", `{"statusCode":200,"data":{"totalCount":1,"list":[{}]}}`, 200, false},
		{"incomplete", `{"statusCode":200,"data":{"totalCount":2,"list":[{"userId":"other"}]}}`, 200, false},
		{"empty-intermediate", `{"statusCode":200,"data":{"totalCount":2,"list":[]}}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			svc := &RoleAssignmentResource{client: c}
			st := objectState(t, svc, roleAssignmentModel("USER"))
			out := resource.ReadResponse{State: st}
			svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
			if tc.removed {
				if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
					t.Fatalf("expected removal: %v", out.Diagnostics)
				}
			} else if !out.Diagnostics.HasError() || out.State.Raw.IsNull() || !out.State.Raw.Equal(st.Raw) {
				t.Fatalf("must retain state with diagnostic: %v %v", out.Diagnostics, out.State.Raw)
			}
		})
	}
}

func TestRoleAssignmentFoundEarlyStillRequiresCompletePagination(t *testing.T) {
	calls := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprintf(w, `{"statusCode":200,"data":{"totalCount":51,"list":[{"userId":"target/one"},%s]}}`, strings.TrimSuffix(strings.Repeat(`{"userId":"other"},`, 49), ","))
		} else {
			fmt.Fprint(w, `{"statusCode":200,"data":{"totalCount":51,"list":[]}}`)
		}
	})
	svc := &RoleAssignmentResource{client: c}
	st := objectState(t, svc, roleAssignmentModel("USER"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if calls != 2 || !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("early match accepted incomplete later page: calls=%d diagnostics=%v", calls, out.Diagnostics)
	}
}

func TestRoleAssignmentUnsupportedTargetReadFailsClosed(t *testing.T) {
	svc := &RoleAssignmentResource{}
	st := objectState(t, svc, roleAssignmentModel("ORG"))
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	if !out.Diagnostics.HasError() || !out.State.Raw.Equal(st.Raw) {
		t.Fatalf("unsupported target must not appear synchronized: %v", out.Diagnostics)
	}
}
