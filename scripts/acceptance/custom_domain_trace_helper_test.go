package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// This tracer is exclusively for httptest: no production host or DNS name is accepted.
func runCustomDomainMockTrace(root, host, old, next string) error {
	u, err := url.Parse(host)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Path != "" || u.Fragment != "" {
		return errors.New("custom domain mock requires an HTTP loopback host")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" || !strings.HasSuffix(old, ".invalid") || !strings.HasSuffix(next, ".invalid") || !sandboxCode.MatchString(strings.TrimSuffix(old, ".invalid")) || !sandboxCode.MatchString(strings.TrimSuffix(next, ".invalid")) || old == next {
		return errors.New("custom domain mock requires distinct generated .invalid domains and loopback host")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	terraform := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, err := os.Stat(terraform); err != nil {
		return errors.New("custom domain phase=terraform-cli (verified Terraform CLI missing)")
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		goBinary = filepath.Join(repo, "../.tools/go/bin/go")
		if _, err = os.Stat(goBinary); err != nil {
			return errors.New("custom domain phase=go-toolchain")
		}
	}
	providerDir := filepath.Join(root, "provider")
	exampleDir := filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(exampleDir, 0700) != nil {
		return errors.New("custom domain phase=workspace")
	}
	cmd := exec.Command(goBinary, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	cmd.Dir = repo
	if _, err = cmd.CombinedOutput(); err != nil {
		return errors.New("custom domain phase=build (output suppressed)")
	}
	config := filepath.Join(root, "terraform.rc")
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", source, providerDir)
	if os.WriteFile(config, []byte(rc), 0600) != nil {
		return errors.New("custom domain phase=config")
	}
	credentials := map[string]string{"AUTHING_ACCESS_KEY_ID": "mock-key", "AUTHING_ACCESS_KEY_SECRET": "mock-secret", "AUTHING_HOST": host}
	env := traceEnvironment(root, config, credentials)
	writeConfig := func(domain string) error {
		hcl := fmt.Sprintf("terraform {\n  required_providers {\n    authing = { source = %q }\n  }\n}\nprovider \"authing\" {}\nresource \"authing_custom_domain\" \"mock\" {\n  custom_domain = %q\n}\n", source, domain)
		return os.WriteFile(filepath.Join(exampleDir, "main.tf"), []byte(hcl), 0600)
	}
	phase := func(name string, f func() error) error {
		if f() != nil {
			return fmt.Errorf("custom domain phase=%s (output suppressed)", name)
		}
		return nil
	}
	run := func(want int, args ...string) error { return terraformExit(root, env, terraform, want, args...) }
	plan := func(want int) error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	// Independent GET against the exact mock singleton after each CLI operation.
	get := func(expected string) error {
		client := &http.Client{Timeout: 5 * time.Second}
		response, e := client.Get(host + "/api/v3/get-custom-domain")
		if e != nil {
			return e
		}
		defer response.Body.Close()
		var result struct {
			StatusCode int `json:"statusCode"`
			Data       struct {
				Domain string `json:"customDomain"`
			} `json:"data"`
		}
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&result) != nil {
			return errors.New("invalid mock GET")
		}
		if expected == "" && result.StatusCode == 404 {
			return nil
		}
		if result.StatusCode != 200 || result.Data.Domain != expected {
			return errors.New("exact mock GET identity mismatch")
		}
		return nil
	}
	if err = phase("initial-absence", func() error { return get("") }); err != nil {
		return err
	}
	if err = phase("config-initial", func() error { return writeConfig(old) }); err != nil {
		return err
	}
	if err = phase("apply-create", func() error { return run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color") }); err != nil {
		return err
	}
	if err = phase("get-created", func() error { return get(old) }); err != nil {
		return err
	}
	if err = phase("read-converged", func() error { return plan(0) }); err != nil {
		return err
	}
	if err = phase("config-replacement", func() error { return writeConfig(next) }); err != nil {
		return err
	}
	saved := filepath.Join(root, "replace.tfplan")
	if err = phase("plan-replacement", func() error {
		return run(2, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode", "-out="+saved)
	}); err != nil {
		return err
	}
	if err = phase("assert-force-new", func() error {
		show := exec.Command(terraform, "show", "-json", saved)
		show.Dir = exampleDir
		show.Env = env
		b, e := show.Output()
		if e != nil {
			return e
		}
		var p struct {
			ResourceChanges []struct {
				Address string `json:"address"`
				Change  struct {
					Actions []string       `json:"actions"`
					Before  map[string]any `json:"before"`
					After   map[string]any `json:"after"`
				} `json:"change"`
			} `json:"resource_changes"`
		}
		if json.Unmarshal(b, &p) != nil || len(p.ResourceChanges) != 1 {
			return errors.New("invalid replacement plan")
		}
		r := p.ResourceChanges[0]
		if r.Address != "authing_custom_domain.mock" || len(r.Change.Actions) != 2 || r.Change.Actions[0] != "delete" || r.Change.Actions[1] != "create" || r.Change.Before["custom_domain"] != old || r.Change.After["custom_domain"] != next {
			return errors.New("not an exact ForceNew replacement")
		}
		return nil
	}); err != nil {
		return err
	}
	if err = phase("apply-replacement", func() error {
		return run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color", saved)
	}); err != nil {
		return err
	}
	if err = phase("get-replaced", func() error { return get(next) }); err != nil {
		return err
	}
	if err = phase("read-replaced", func() error { return plan(0) }); err != nil {
		return err
	}
	if err = phase("destroy", func() error { return run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color") }); err != nil {
		return err
	}
	return phase("exact-get-absence", func() error { return get("") })
}
