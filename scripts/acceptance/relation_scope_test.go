package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDepartmentScopeCompletesSecondPageAfterMatch(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		if r.URL.Path != "/api/v3/list-department-members" || r.URL.Query().Get("organizationCode") != "org" || r.URL.Query().Get("departmentId") != "dept" || r.URL.Query().Get("includeChildrenDepartments") != "false" {
			http.Error(w, "wrong scope", 500)
			return
		}
		calls++
		list := []any{}
		if r.URL.Query().Get("page") == "1" {
			list = append(list, map[string]string{"userId": "user"})
			for i := 1; i < 100; i++ {
				list = append(list, map[string]string{"userId": fmt.Sprintf("other-%d", i)})
			}
		} else if r.URL.Query().Get("page") == "2" {
			list = append(list, map[string]string{"userId": "last"})
		} else {
			http.Error(w, "unexpected page", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": 101, "list": list}})
	}))
	defer s.Close()
	c, e := policyTestClient(s.URL)
	if e != nil {
		t.Fatal(e)
	}
	got, e := directDepartmentMember(c, "org", "dept", "user")
	if e != nil || !got || calls != 2 {
		t.Fatalf("did not complete direct scope: found=%v err=%v pages=%d", got, e, calls)
	}
}

// Even a match on page one cannot prove a complete scoped inventory.
func TestRelationScopeRejectsIncompleteAndWrongType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []map[string]string
		total   int
		want    bool
	}{
		{"complete", []map[string]string{{"targetIdentifier": "subject", "targetType": "GROUP"}, {"targetIdentifier": "subject", "targetType": "USER"}}, 2, true},
		{"wrong-type", []map[string]string{{"targetIdentifier": "subject", "targetType": "GROUP"}}, 1, false},
		{"truncated-after-match", []map[string]string{{"targetIdentifier": "subject", "targetType": "USER"}}, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-management-token" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
					return
				}
				if r.URL.Path != "/api/v3/list-data-policy-targets" || r.URL.Query().Get("policyId") != "policy" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "50" {
					http.Error(w, "invalid scope", 500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"statusCode": 200, "data": map[string]any{"totalCount": tc.total, "list": tc.entries}})
			}))
			defer s.Close()
			c, e := policyTestClient(s.URL)
			if e != nil {
				t.Fatal(e)
			}
			got, e := exactPolicyTarget(c, "policy", "USER", "subject")
			if tc.name == "truncated-after-match" {
				if e == nil {
					t.Fatal("accepted incomplete page")
				}
				return
			}
			if e != nil || got != tc.want {
				t.Fatalf("scope result=%v err=%v", got, e)
			}
		})
	}
}
