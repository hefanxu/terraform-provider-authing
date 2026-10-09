package acceptance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// Offline only: never create a tenant or apply a replacement against Authing.
func TestExtIdpOfflineCompositeImportAndReplacement(t *testing.T) {
	var writes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"mock-token","expires_in":3600}}`)
			return
		}
		if r.URL.Path != "/api/v3/get-ext-idp" || r.Method != "GET" {
			writes.Add(1)
			http.Error(w, "unexpected mutation", 500)
			return
		}
		if r.URL.Query().Get("id") != "idp-1" {
			fmt.Fprint(w, `{"statusCode":404}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"id":"idp-1","name":"Original","type":"oidc","tenantId":"tenant-A","connections":[]}}`)
	}))
	defer server.Close()
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	tf := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	root := t.TempDir()
	bin := filepath.Join(root, "provider")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command(goBin, "build", "-o", filepath.Join(bin, "terraform-provider-authing"), ".")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, bin)
	if err := os.WriteFile(config, []byte(rc), 0600); err != nil {
		t.Fatal(err)
	}
	creds := idpCredentials(server)
	setup := func(dir, kind, tenant string) []string {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "example"), 0700); err != nil {
			t.Fatal(err)
		}
		hcl := fmt.Sprintf("terraform {\n required_providers { authing = { source = %q } }\n}\nprovider \"authing\" {}\nresource \"authing_ext_idp\" \"sandbox\" {\n name = \"Original\"\n type = %q\n tenant_id = %q\n}\n", source, kind, tenant)
		if err := os.WriteFile(filepath.Join(dir, "example/main.tf"), []byte(hcl), 0600); err != nil {
			t.Fatal(err)
		}
		return traceEnvironment(dir, config, creds)
	}
	scoped := filepath.Join(root, "scoped")
	env := setup(scoped, "oidc", "tenant-A")
	if err := terraformExit(scoped, env, tf, 0, "import", "-input=false", "-no-color", "authing_ext_idp.sandbox", "tenant-A:idp-1"); err != nil {
		t.Fatal("scoped import failed")
	}
	if err := terraformExit(scoped, env, tf, 0, "plan", "-input=false", "-no-color", "-detailed-exitcode"); err != nil {
		t.Fatal("scoped import did not converge")
	}
	for _, tc := range []struct{ field, kind, tenant string }{{"type", "saml", "tenant-A"}, {"tenant_id", "oidc", "tenant-B"}} {
		t.Run(tc.field, func(t *testing.T) {
			setup(scoped, tc.kind, tc.tenant)
			if err := terraformExit(scoped, env, tf, 2, "plan", "-refresh=false", "-input=false", "-no-color", "-detailed-exitcode", "-out=replace.tfplan"); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(tf, "show", "-json", "replace.tfplan")
			cmd.Dir = filepath.Join(scoped, "example")
			cmd.Env = env
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var plan struct {
				Changes []struct {
					Address string `json:"address"`
					Change  struct {
						Actions      []string   `json:"actions"`
						ReplacePaths [][]string `json:"replace_paths"`
					} `json:"change"`
				} `json:"resource_changes"`
			}
			if err := json.Unmarshal(out, &plan); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range plan.Changes {
				if r.Address == "authing_ext_idp.sandbox" {
					found = strings.Join(r.Change.Actions, ",") == "delete,create"
					matched := false
					for _, p := range r.Change.ReplacePaths {
						if len(p) == 1 && p[0] == tc.field {
							matched = true
						}
					}
					if !matched {
						t.Fatalf("missing replacement path %s", tc.field)
					}
				}
			}
			if !found {
				t.Fatal("immutable field was not replacement")
			}
		})
	}
	for _, id := range []string{"idp-1", "tenant-B:idp-1", ":idp-1", "tenant-A:", "a:b:c"} {
		dir := t.TempDir()
		env := setup(dir, "oidc", "tenant-A")
		if err := terraformExit(dir, env, tf, 1, "import", "-input=false", "-no-color", "authing_ext_idp.sandbox", id); err != nil {
			t.Fatalf("unsafe import accepted %q", id)
		}
	}
	if writes.Load() != 0 {
		t.Fatal("import or replacement plan attempted writes")
	}
}
