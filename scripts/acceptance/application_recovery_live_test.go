package acceptance

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

var applicationRecoveryLive = flag.Bool("authing-application-recovery", false, "read-only incident application ownership probe")

func applicationRecoveryGuard(code string, env map[string]string) error {
	if (code != incidentApplicationCode && code != secondIncidentApplicationCode && code != thirdIncidentApplicationCode && code != fourthIncidentApplicationCode) || !sandboxCode.MatchString(code) || env["AUTHING_ACCEPTANCE_CONFIRM"] != "READ_ONLY_SANDBOX" || env["AUTHING_ACCESS_KEY_ID"] == "" || env["AUTHING_ACCESS_KEY_SECRET"] == "" {
		return errors.New("read-only application recovery requires exact incident code, confirmation, and sandbox credentials")
	}
	return nil
}

// probeApplication never writes. Unknown response, foreign match, or incomplete
// pagination is ambiguous, not evidence of absence or permission to delete.
func probeApplication(client *authingapi.Client, code string) string {
	if !sandboxCode.MatchString(code) {
		return "ambiguous"
	}
	seen := 0
	matches := map[string]bool{}
	for page := 1; page <= 100; page++ {
		body, err := client.SendHttpRequest("/api/v3/list-applications", "GET", map[string]any{"page": page, "limit": 100})
		if err != nil {
			return "ambiguous"
		}
		var res struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				List []struct {
					AppId         string `json:"appId"`
					AppName       string `json:"appName"`
					AppIdentifier string `json:"appIdentifier"`
				} `json:"list"`
				TotalCount *int `json:"totalCount"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &res) != nil || res.StatusCode != 200 || res.Data == nil || res.Data.List == nil || res.Data.TotalCount == nil || *res.Data.TotalCount < 0 || len(res.Data.List) > 100 || seen+len(res.Data.List) > *res.Data.TotalCount {
			return "ambiguous"
		}
		for _, a := range res.Data.List {
			if a.AppName == code || a.AppIdentifier == code {
				if a.AppId == "" || matches[a.AppId] {
					return "ambiguous"
				}
				matches[a.AppId] = true
			}
		}
		seen += len(res.Data.List)
		if seen == *res.Data.TotalCount {
			if len(matches) == 0 {
				return "absent"
			}
			if len(matches) != 1 {
				return "ambiguous"
			}
			for id := range matches {
				got := client.GetApplication(&dto.GetApplicationDto{AppId: id})
				if got == nil || got.StatusCode != 200 || got.Data.AppId != id || got.Data.AppIdentifier != code || got.Data.AppDescription != "hermesacc ownership "+code || got.Data.AppType != "web" || got.Data.AppName != code && got.Data.AppName != code+"-drift" {
					return "ambiguous"
				}
			}
			return "owned"
		}
		if len(res.Data.List) == 0 {
			return "ambiguous"
		}
	}
	return "ambiguous"
}

func TestReadOnlyApplicationRecovery(t *testing.T) {
	if !*applicationRecoveryLive {
		t.Skip("requires explicit read-only incident recovery flag")
	}
	code := os.Getenv("ACCEPTANCE_TEST_OBJECT_CODE")
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if err := applicationRecoveryGuard(code, env); err != nil {
		t.Fatal("application recovery guard rejected input (output suppressed)")
	}
	client, err := authingapi.NewClient(authingapi.Options{AccessKeyID: env["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: env["AUTHING_ACCESS_KEY_SECRET"], Host: env["AUTHING_HOST"]})
	if err != nil {
		t.Fatal("application recovery client failed (output suppressed)")
	}
	status := probeApplication(client, code)
	fmt.Printf("application-recovery code=%s result=%s\n", code, status)
	if status == "ambiguous" {
		t.Fatal("application recovery inconclusive (output suppressed)")
	}
}
