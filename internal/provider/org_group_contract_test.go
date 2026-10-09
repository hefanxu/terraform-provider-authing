package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestOrganizationDeleteRejectsUnsafePreflight(t *testing.T) {
	for _, tc := range []struct {
		name, pre string
		missing   bool
	}{
		{"children", `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Organization","hasChildren":true}}`, false},
		{"unknownChildren", `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Organization"}}`, false},
		{"wrongIdentity", `{"statusCode":200,"data":{"organizationCode":"other","organizationName":"Organization","hasChildren":false}}`, false},
		{"emptyData", `{"statusCode":200,"data":null}`, false},
		{"getFailure", `{"statusCode":500}`, false},
		{"missing", `{"statusCode":404}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes, reads := 0, 0
			client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/get-organization":
					reads++
					if r.Method != http.MethodGet || r.URL.Query().Get("organizationCode") != "org" {
						t.Errorf("wrong preflight %s %s", r.Method, r.URL)
					}
					fmt.Fprint(w, tc.pre)
				case "/api/v3/delete-organization":
					writes++
					fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			svc := &OrganizationResource{client: client}
			st := lifecycleState(t, svc, OrganizationModel{ID: types.StringValue("org"), OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("Organization")})
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if writes != 0 || reads != 1 || out.Diagnostics.HasError() == tc.missing {
				t.Fatalf("reads=%d writes=%d diagnostics=%v", reads, writes, out.Diagnostics)
			}
		})
	}
}

func TestOrganizationDeleteExactIdentityAndPostcheck(t *testing.T) {
	for _, tc := range []struct {
		name, after string
		wantError   bool
	}{
		{"confirmed", `{"statusCode":404}`, false},
		{"stillPresent", `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Organization","hasChildren":false}}`, true},
		{"verificationFailed", `{"statusCode":500}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, writes := 0, 0
			client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/get-organization":
					reads++
					if r.URL.Query().Get("organizationCode") != "org" {
						t.Errorf("wrong lookup: %s", r.URL)
					}
					if reads == 1 {
						fmt.Fprint(w, `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Organization","hasChildren":false}}`)
					} else {
						fmt.Fprint(w, tc.after)
					}
				case "/api/v3/delete-organization":
					writes++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["organizationCode"] != "org" {
						t.Errorf("wrong deletion: %v %v", body, err)
					}
					fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			})
			svc := &OrganizationResource{client: client}
			st := lifecycleState(t, svc, OrganizationModel{ID: types.StringValue("org"), OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("Organization")})
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if reads != 2 || writes != 1 || out.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("reads=%d writes=%d diagnostics=%v", reads, writes, out.Diagnostics)
			}
		})
	}
}

func TestOrganizationDeleteRejectsMismatchedStateID(t *testing.T) {
	requests := 0
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `{"statusCode":200,"data":{"organizationCode":"org","hasChildren":false}}`)
	})
	svc := &OrganizationResource{client: client}
	st := lifecycleState(t, svc, OrganizationModel{ID: types.StringValue("different"), OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("Organization")})
	out := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
	if !out.Diagnostics.HasError() || requests != 0 {
		t.Fatalf("mismatched ID allowed requests=%d diagnostics=%v", requests, out.Diagnostics)
	}
}

func TestOrganizationCreateRequiredContract(t *testing.T) {
	svc := &OrganizationResource{}
	var sr resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	attr, ok := sr.Schema.Attributes["organization_code"].(schema.StringAttribute)
	if !ok || !attr.Required {
		t.Fatal("organization_code must be required for create")
	}
	var body map[string]json.RawMessage
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/create-organization" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"organizationCode":"org","organizationName":"Organization"}}`)
	})
	svc.client = client
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(context.Background(), OrganizationModel{OrganizationCode: types.StringValue("org"), OrganizationName: types.StringValue("Organization")}); d.HasError() {
		t.Fatal(d)
	}
	out := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
	svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	if string(body["metadata"]) != "{}" || string(body["organizationCode"]) != `"org"` || string(body["organizationName"]) != `"Organization"` {
		t.Fatalf("invalid create body: %v", body)
	}
}

func TestGroupCreateRequiredContract(t *testing.T) {
	svc := &GroupResource{}
	var sr resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	attr, ok := sr.Schema.Attributes["type"].(schema.StringAttribute)
	if !ok || !attr.Required {
		t.Fatal("group type must be supplied by user; no documented default enum")
	}
	for _, tc := range []struct {
		name        string
		description types.String
		want        string
	}{{"provided", types.StringValue("Platform team"), "Platform team"}, {"omitted", types.StringNull(), ""}} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			svc.client = lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-group" {
					fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","type":"static","description":"`+tc.want+`"}}`)
					return
				}
				if r.URL.Path != "/api/v3/create-group" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","type":"static","description":"`+tc.want+`"}}`)
			})
			plan := tfsdk.Plan{Schema: sr.Schema}
			if d := plan.Set(context.Background(), GroupModel{Code: types.StringValue("engineering"), Name: types.StringValue("Engineering"), Type: types.StringValue("static"), Description: tc.description}); d.HasError() {
				t.Fatal(d)
			}
			out := resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema}}
			svc.Create(context.Background(), resource.CreateRequest{Plan: plan}, &out)
			if out.Diagnostics.HasError() {
				t.Fatal(out.Diagnostics)
			}
			if string(body["type"]) != `"static"` || string(body["description"]) != fmt.Sprintf("%q", tc.want) {
				t.Fatalf("invalid create body: %v", body)
			}
		})
	}
}

func TestGroupTypeSchemaRejectsBlank(t *testing.T) {
	svc := &GroupResource{}
	var sr resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	attr := sr.Schema.Attributes["type"].(schema.StringAttribute)
	if len(attr.Validators) == 0 {
		t.Fatal("type needs a schema validator; empty string is not a type")
	}
	for _, v := range []string{"", "  "} {
		result := validator.StringResponse{}
		attr.Validators[0].ValidateString(context.Background(), validator.StringRequest{Path: path.Root("type"), ConfigValue: types.StringValue(v)}, &result)
		if !result.Diagnostics.HasError() {
			t.Fatalf("blank type %q accepted", v)
		}
	}
}

func TestGroupUpdateSendsRequiredDescription(t *testing.T) {
	var body map[string]json.RawMessage
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/get-group" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","type":"static","description":""}}`)
			return
		}
		if r.URL.Path != "/api/v3/update-group" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","description":""}}`)
	})
	svc := &GroupResource{client: client}
	var sr resource.SchemaResponse
	svc.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(context.Background(), GroupModel{ID: types.StringValue("engineering"), Code: types.StringValue("engineering"), Name: types.StringValue("Engineering"), Type: types.StringValue("static"), Description: types.StringNull()}); d.HasError() {
		t.Fatal(d)
	}
	out := resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema}}
	svc.Update(context.Background(), resource.UpdateRequest{Plan: plan}, &out)
	if out.Diagnostics.HasError() {
		t.Fatal(out.Diagnostics)
	}
	if string(body["code"]) != `"engineering"` || string(body["description"]) != `""` {
		t.Fatalf("invalid update: %v", body)
	}
	if !strings.Contains(fmt.Sprint(body), "description") {
		t.Fatal("description missing")
	}
	var state GroupModel
	if d := out.State.Get(context.Background(), &state); d.HasError() || state.Description.IsNull() || state.Description.ValueString() != "" {
		t.Fatalf("empty description should round-trip: %v / %#v", d, state.Description)
	}
}

func TestGroupReadEmptyDescriptionRoundTrips(t *testing.T) {
	client := lifecycleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-group" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","type":"static","description":""}}`)
	})
	svc := &GroupResource{client: client}
	st := lifecycleState(t, svc, GroupModel{ID: types.StringValue("engineering"), Code: types.StringValue("engineering"), Name: types.StringValue("Engineering"), Type: types.StringValue("static"), Description: types.StringValue("")})
	out := resource.ReadResponse{State: st}
	svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
	var state GroupModel
	if d := out.State.Get(context.Background(), &state); out.Diagnostics.HasError() || d.HasError() || state.Description.IsNull() || state.Description.ValueString() != "" {
		t.Fatalf("empty description lost: %v / %#v", out.Diagnostics, state.Description)
	}
}
