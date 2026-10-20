package acceptance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"terraform-provider-authing/internal/authingapi"
	"testing"
)

func TestGroupPinnedCleanupRejectsUnsafeInventories(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	for _, body := range []string{`{"statusCode":200,"data":{"totalCount":1,"list":[]}}`, `{"statusCode":200,"data":{"totalCount":0}}`, `{"statusCode":200,"data":{"list":[]}}`, `{"statusCode":403}`, `{"statusCode":422}`, `{"statusCode":500}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			writes := 0
			g := &mockGroup{code: code, name: code, description: "hermesacc ownership " + code, kind: "static"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/list-group-members" {
					fmt.Fprint(w, body)
					return
				}
				if r.URL.Path == "/api/v3/delete-groups-batch" {
					writes++
				}
				g.serve(w, r)
			}))
			defer server.Close()
			c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock", AccessKeySecret: "mock", Host: server.URL})
			if e != nil {
				t.Fatal(e)
			}
			if cleanupPinnedGroup(c, code, code) == nil || writes != 0 {
				t.Fatal("unsafe inventory allowed deletion")
			}
		})
	}
}
func TestGroupPinnedCleanupRejectsForeignOrUnpinnedIdentity(t *testing.T) {
	code := "hermesacc-1234567890abcdef"
	for _, mode := range []string{"unpinned", "foreign-code", "foreign-name", "mixed-marker", "foreign-type"} {
		t.Run(mode, func(t *testing.T) {
			g := &mockGroup{code: code, name: code, description: "hermesacc ownership " + code, kind: "static"}
			id := code
			switch mode {
			case "unpinned":
				id = ""
			case "foreign-code":
				g.code = "foreign"
			case "foreign-name":
				g.name = "foreign"
			case "mixed-marker":
				g.description += "-drift"
			case "foreign-type":
				g.kind = "foreign"
			}
			server := httptest.NewServer(http.HandlerFunc(g.serve))
			defer server.Close()
			c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: "mock", AccessKeySecret: "mock", Host: server.URL})
			if e != nil {
				t.Fatal(e)
			}
			e = cleanupPinnedGroup(c, id, code)
			// An explicit 404 at the pinned code proves absence, not ownership of a foreign code.
			if mode != "foreign-code" && e == nil {
				t.Fatal("ownership accepted")
			}
			if g.deletes != 0 {
				t.Fatal("foreign deletion")
			}
		})
	}
}
