package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/dto"
	"terraform-provider-authing/internal/authingapi"
)

func directDepartmentMember(c *authingapi.Client, org, dept, user string) (bool, error) {
	seen := map[string]bool{}
	total := -1
	found := false
	for page := 1; page <= 10000; page++ {
		raw, err := c.SendHttpRequest("/api/v3/list-department-members", http.MethodGet, map[string]any{"organizationCode": org, "departmentId": dept, "page": page, "limit": 100, "includeChildrenDepartments": false})
		var out struct {
			StatusCode int `json:"statusCode"`
			Data       *struct {
				Total *int `json:"totalCount"`
				List  *[]struct {
					UserID string `json:"userId"`
				} `json:"list"`
			} `json:"data"`
		}
		if err != nil || json.Unmarshal(raw, &out) != nil || out.StatusCode != 200 || out.Data == nil || out.Data.Total == nil || out.Data.List == nil || *out.Data.Total < 0 {
			return false, errors.New("department member inventory incomplete")
		}
		if total < 0 {
			total = *out.Data.Total
		}
		if total != *out.Data.Total {
			return false, errors.New("department member count changed")
		}
		n := total - len(seen)
		if n > 100 {
			n = 100
		}
		if n < 0 || len(*out.Data.List) != n {
			return false, errors.New("department member page incomplete")
		}
		for _, v := range *out.Data.List {
			if v.UserID == "" || seen[v.UserID] {
				return false, errors.New("department member identity invalid")
			}
			seen[v.UserID] = true
			if v.UserID == user {
				found = true
			}
		}
		if len(seen) == total {
			return found, nil
		}
	}
	return false, errors.New("department member pagination unbounded")
}
func runDepartmentMemberTrace(root string, creds map[string]string, org, openID, username string) (result error) {
	if !sandboxCode.MatchString(org) || !sandboxCode.MatchString(openID) || !sandboxCode.MatchString(username) {
		return errors.New("invalid generated identities")
	}
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	tf := filepath.Join(repo, "../.tools/terraform/1.13.5/terraform")
	if _, e := os.Stat(tf); e != nil {
		return errors.New("Terraform CLI unavailable")
	}
	goBin, e := exec.LookPath("go")
	if e != nil {
		goBin = filepath.Join(repo, "../.tools/go/bin/go")
		if _, e = os.Stat(goBin); e != nil {
			return errors.New("Go unavailable")
		}
	}
	c, e := authingapi.NewClient(authingapi.Options{AccessKeyID: creds["AUTHING_ACCESS_KEY_ID"], AccessKeySecret: creds["AUTHING_ACCESS_KEY_SECRET"], Host: creds["AUTHING_HOST"]})
	if e != nil {
		return errors.New("client unavailable")
	}
	if x := c.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: org}); x == nil || x.StatusCode != 404 {
		return errors.New("organization preflight failed")
	}
	if x := c.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: openID}); x == nil || x.StatusCode != 404 {
		return errors.New("department preflight failed")
	}
	providerDir, example := filepath.Join(root, "provider"), filepath.Join(root, "example")
	if os.MkdirAll(providerDir, 0700) != nil || os.MkdirAll(example, 0700) != nil {
		return errors.New("workspace unavailable")
	}
	build := exec.Command(goBin, "build", "-o", filepath.Join(providerDir, "terraform-provider-authing"), ".")
	build.Dir = repo
	if _, e = build.CombinedOutput(); e != nil {
		return errors.New("provider build failed")
	}
	rcPath := filepath.Join(root, "terraform.rc")
	hcl := fmt.Sprintf(`terraform {
 required_providers {
  authing = { source = %q }
 }
}
provider "authing" {}
resource "authing_organization" "sandbox" {
 organization_code = %q
 organization_name = %q
 description = %q
}
resource "authing_department" "sandbox" {
 organization_code = authing_organization.sandbox.organization_code
 department_id = %q
 name = %q
 parent_department_id = "root"
 description = %q
}
resource "authing_user" "sandbox" {
 username = %q
 nickname = %q
}
resource "authing_department_member" "sandbox" {
 organization_code = authing_organization.sandbox.organization_code
 department_id = authing_department.sandbox.id
 user_id = authing_user.sandbox.id
}
`, source, org, org, "hermesacc ownership "+org, openID, openID, "hermesacc ownership "+openID, username, username)
	if os.WriteFile(rcPath, []byte(fmt.Sprintf("provider_installation {\n dev_overrides { %q = %q }\n direct {}\n}\n", source, providerDir)), 0600) != nil || os.WriteFile(filepath.Join(example, "main.tf"), []byte(hcl), 0600) != nil {
		return errors.New("config unavailable")
	}
	env := traceEnvironment(root, rcPath, creds)
	run := func(want int, args ...string) func() error {
		return func() error { return terraformExit(root, env, tf, want, args...) }
	}
	plan := func(want int) func() error {
		return run(want, "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	}
	user, dept := "", ""
	name := openID
	defer func() {
		if user == "" {
			user, _ = userStateID(root, tf, env, username)
		}
		if dept == "" {
			dept, _ = departmentStateID(root, tf, env, org, openID)
		}
		if user == "" || dept == "" {
			if result != nil {
				result = fmt.Errorf("%v; cleanup=manual-review (missing state-backed identity)", result)
			}
			return
		}
		u := c.GetUser(&dto.GetUserDto{UserId: user})
		if u == nil || u.StatusCode != 200 && u.StatusCode != 404 {
			result = fmt.Errorf("%v; cleanup=incomplete (user identity)", result)
			return
		}
		if u.StatusCode == 200 {
			if e := verifyOwnedUser(c, user, username); e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (user identity)", result)
				return
			}
		}
		if e := departmentGet(c, org, dept, openID, name, "hermesacc ownership "+openID); e == nil {
			present, e := directDepartmentMember(c, org, dept, user)
			if e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (member inventory)", result)
				return
			}
			if present {
				x := c.RemoveDepartmentMembers(&dto.RemoveDepartmentMembersReqDto{OrganizationCode: org, DepartmentId: dept, UserIds: []string{user}})
				if x == nil || x.StatusCode != 200 || !x.Data.Success {
					result = fmt.Errorf("%v; cleanup=incomplete (revoke)", result)
					return
				}
			}
			if e := departmentEmpty(c, org, dept, openID, name, "hermesacc ownership "+openID); e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (department occupied)", result)
				return
			}
			if e := cleanupDepartmentClient(c, org, dept, openID, name, "hermesacc ownership "+openID); e != nil {
				result = fmt.Errorf("%v; cleanup=incomplete (department)", result)
				return
			}
		} else if x := c.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: dept}); x == nil || x.StatusCode != 404 {
			result = fmt.Errorf("%v; cleanup=incomplete (department identity)", result)
			return
		}
		if e := cleanupUser(c, user, username); e != nil {
			result = fmt.Errorf("%v; cleanup=incomplete (user)", result)
			return
		}
		if e := cleanupOrganization(c, org, org, "hermesacc ownership "+org); e != nil {
			result = fmt.Errorf("%v; cleanup=incomplete (organization)", result)
		}
	}()
	verify := func(want bool) error {
		got, e := directDepartmentMember(c, org, dept, user)
		if e != nil || got != want {
			return errors.New("direct department membership not verified")
		}
		return nil
	}
	result = (traceCase{name: "department-member", code: org, phases: []tracePhase{
		{"apply-user", run(0, "apply", "-target=authing_user.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-user", func() error { var e error; user, e = userStateID(root, tf, env, username); return e }},
		{"apply-department", run(0, "apply", "-target=authing_department.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"capture-department", func() error { var e error; dept, e = departmentStateID(root, tf, env, org, openID); return e }},
		{"verify-parents", func() error {
			if e := verifyOwnedUser(c, user, username); e != nil {
				return e
			}
			return departmentGet(c, org, dept, openID, name, "hermesacc ownership "+openID)
		}},
		{"apply-relation", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-relation-state", func() error {
			return verifyRelationState(root, tf, env, "authing_department_member.sandbox", org+":"+dept+":"+user, map[string]string{"organization_code": org, "department_id": dept, "user_id": user})
		}},
		{"verify-attached", func() error { return verify(true) }},
		{"plan-converged", plan(0)},
		{"remote-revoke", func() error {
			x := c.RemoveDepartmentMembers(&dto.RemoveDepartmentMembersReqDto{OrganizationCode: org, DepartmentId: dept, UserIds: []string{user}})
			if x == nil || x.StatusCode != 200 || !x.Data.Success {
				return errors.New("remote revoke failed")
			}
			return verify(false)
		}},
		{"plan-drift", plan(2)},
		{"apply-reconcile", run(0, "apply", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"plan-reconverged", plan(0)},
		{"verify-restored", func() error { return verify(true) }},
		{"destroy-relation", run(0, "destroy", "-target=authing_department_member.sandbox", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-user-preserved", func() error {
			if e := verify(false); e != nil {
				return e
			}
			if e := verifyOwnedUser(c, user, username); e != nil {
				return e
			}
			return departmentEmpty(c, org, dept, openID, name, "hermesacc ownership "+openID)
		}},
		{"destroy-parents", run(0, "destroy", "-auto-approve", "-lock=false", "-input=false", "-no-color")},
		{"verify-absence", func() error {
			if x := c.GetUser(&dto.GetUserDto{UserId: user}); x == nil || x.StatusCode != 404 {
				return errors.New("user absence unverified")
			}
			if x := c.GetDepartment(&dto.GetDepartmentDto{OrganizationCode: org, DepartmentId: dept}); x == nil || x.StatusCode != 404 {
				return errors.New("department absence unverified")
			}
			if x := c.GetOrganization(&dto.GetOrganizationDto{OrganizationCode: org}); x == nil || x.StatusCode != 404 {
				return errors.New("organization absence unverified")
			}
			return nil
		}},
	}}).execute()
	return result
}
func TestDestructiveLiveDepartmentMemberTrace(t *testing.T) {
	if !*destructiveLive {
		t.Skip("requires destructive sandbox opt-in")
	}
	env := map[string]string{"AUTHING_ACCEPTANCE_CONFIRM": os.Getenv("AUTHING_ACCEPTANCE_CONFIRM"), "AUTHING_ACCESS_KEY_ID": os.Getenv("AUTHING_ACCESS_KEY_ID"), "AUTHING_ACCESS_KEY_SECRET": os.Getenv("AUTHING_ACCESS_KEY_SECRET"), "AUTHING_HOST": os.Getenv("AUTHING_HOST")}
	if e := destructiveGuard(*destructiveLive, env); e != nil {
		t.Fatal(e)
	}
	org, e := newGroupCode()
	if e != nil {
		t.Fatal("identity generation failed")
	}
	dept, e := newGroupCode()
	if e != nil || dept == org {
		t.Fatal("identity generation failed")
	}
	user, e := newGroupCode()
	if e != nil || user == dept || user == org {
		t.Fatal("identity generation failed")
	}
	if e := runDepartmentMemberTrace(t.TempDir(), env, org, dept, user); e != nil {
		t.Fatal(e)
	}
}
