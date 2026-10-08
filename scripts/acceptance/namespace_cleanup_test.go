package acceptance

import (
	"flag"
	"os"
	"testing"

	"terraform-provider-authing/internal/authingapi"
)

const incidentNamespaceCode = "hermesacc-a69a5cb1b971aa8e"

var namespaceCleanupLive = flag.Bool("authing-namespace-cleanup-sandbox", false, "opt in to deletion of the exact test-owned incident namespace")

func namespaceCleanupGuard(enabled bool, code string, env map[string]string) bool {
	return enabled && code == incidentNamespaceCode && sandboxCode.MatchString(code) &&
		env["AUTHING_ACCEPTANCE_CONFIRM"] == "DESTRUCTIVE_SANDBOX" &&
		env["AUTHING_ACCESS_KEY_ID"] != "" && env["AUTHING_ACCESS_KEY_SECRET"] != ""
}

func TestNamespaceCleanupGuard(t *testing.T) {
	base := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}
	if !namespaceCleanupGuard(true, incidentNamespaceCode, base) {
		t.Fatal("valid cleanup guard rejected")
	}
	for _, tc := range []struct {
		flag bool
		code string
		env  map[string]string
	}{
		{false, incidentNamespaceCode, base},
		{true, "default", base},
		{true, "hermesacc-1234567890abcdef", base},
		{true, incidentNamespaceCode, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "READ_ONLY_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key", "AUTHING_ACCESS_KEY_SECRET": "secret"}},
		{true, incidentNamespaceCode, map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX", "AUTHING_ACCESS_KEY_ID": "key"}},
	} {
		if namespaceCleanupGuard(tc.flag, tc.code, tc.env) {
			t.Fatal("cleanup guard accepted unsafe request")
		}
	}
}

func TestNamespaceRecoveryCleanupMockSuccess(t *testing.T) {
	mock := &mockNamespace{code: namespaceTestCode, name: namespaceTestCode, description: "hermesacc ownership " + namespaceTestCode}
	client, server := namespaceClient(t, mock)
	defer server.Close()
	if err := cleanupNamespace(client, namespaceTestCode, namespaceTestCode, "hermesacc ownership "+namespaceTestCode); err != nil {
		t.Fatal("owned empty namespace cleanup failed")
	}
	if mock.deletes != 1 || mock.code != "" || probeNamespaceRecoveryDiagnostic(client, namespaceTestCode).Status != "absent" {
		t.Fatal("namespace deletion or readback was not confirmed")
	}
}

func TestNamespaceRecoveryCleanup(t *testing.T) {
	if !*namespaceCleanupLive {
		t.Skip("requires exact incident code and explicit namespace cleanup flag")
	}
	code := os.Getenv("AUTHING_TEST_OBJECT_CODE")
	env := map[string]string{
		"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"),
		"AUTHING_ACCESS_KEY_ID":      os.Getenv("AUTHING_ACCESS_KEY_ID"),
		"AUTHING_ACCESS_KEY_SECRET":  os.Getenv("AUTHING_ACCESS_KEY_SECRET"),
	}
	if !namespaceCleanupGuard(*namespaceCleanupLive, code, env) {
		t.Fatal("namespace cleanup guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: os.Getenv("AUTHING_HOST")})
	if err != nil {
		t.Fatal("namespace cleanup client setup failed (output suppressed)")
	}
	if err := cleanupNamespace(client, code, code, "hermesacc ownership "+code); err != nil {
		t.Fatalf("namespace cleanup refused or failed for %s (output suppressed)", code)
	}
	if got := probeNamespaceRecoveryDiagnostic(client, code); got.Status != "absent" {
		t.Fatalf("namespace absence not confirmed for %s (output suppressed)", code)
	}
	t.Logf("namespace cleanup result=absent code=%s", code)
}
