package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const domain = "sso.example.com"
const secret = "PRIVATE-KEY-MUST-NEVER-LEAK"
const certificate = "CERT-MUST-NEVER-LEAK"
const domainResponse = `{"statusCode":200,"data":{"customDomain":"sso.example.com","dnsTxtName":"_verify.sso.example.com","dnsTxtValue":"verify-value","dnsVerified":true,"cname":"edge.example.com","httpsVerified":false,"httpsPrivateKey":"` + secret + `","httpsCertificate":"` + certificate + `"}}`

func domainModel() CustomDomainModel {
	return CustomDomainModel{ID: types.StringUnknown(), CustomDomain: types.StringValue(domain), DNSTxtName: types.StringUnknown(), DNSTxtValue: types.StringUnknown(), DNSVerified: types.BoolUnknown(), CNAME: types.StringUnknown(), HTTPSVerified: types.BoolUnknown()}
}
func domainStateModel() CustomDomainModel {
	m := domainModel()
	m.ID = types.StringValue(domain)
	m.DNSTxtName = types.StringValue("_verify.sso.example.com")
	m.DNSTxtValue = types.StringValue("verify-value")
	m.DNSVerified = types.BoolValue(true)
	m.CNAME = types.StringValue("edge.example.com")
	m.HTTPSVerified = types.BoolValue(false)
	return m
}
func TestCustomDomainLifecycleAndSecretFiltering(t *testing.T) {
	ctx := context.Background()
	present, gets, creates, removes := false, 0, 0, 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/get-custom-domain":
			gets++
			if r.Method != http.MethodGet || r.URL.RawQuery != "" {
				t.Errorf("unexpected get %s %s", r.Method, r.URL.RawQuery)
			}
			if present {
				fmt.Fprint(w, domainResponse)
			} else {
				fmt.Fprint(w, `{"statusCode":404}`)
			}
		case "/api/v3/create-custom-domain":
			creates++
			b, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || strings.TrimSpace(string(b)) != `{"customDomain":"sso.example.com"}` {
				t.Errorf("unexpected create %s %s", r.Method, b)
			}
			present = true
			fmt.Fprint(w, `{"statusCode":200,"data":{"customDomain":"sso.example.com"}}`)
		case "/api/v3/remove-custom-domain":
			removes++
			b, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || len(b) != 0 {
				t.Errorf("unexpected remove %s %q", r.Method, b)
			}
			present = false
			fmt.Fprint(w, `{"statusCode":200,"data":{"success":true}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	svc := &CustomDomainResource{client: c}
	created := resource.CreateResponse{State: objectState(t, svc, &CustomDomainModel{})}
	svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, domainModel())}, &created)
	if created.Diagnostics.HasError() || creates != 1 || gets != 2 {
		t.Fatalf("create %v gets=%d creates=%d", created.Diagnostics, gets, creates)
	}
	var m CustomDomainModel
	if d := created.State.Get(ctx, &m); d.HasError() || m.ID.ValueString() != domain || m.DNSTxtValue.ValueString() != "verify-value" || !m.DNSVerified.ValueBool() || m.HTTPSVerified.ValueBool() {
		t.Fatalf("state %+v %v", m, d)
	}
	if strings.Contains(fmt.Sprint(m), secret) || strings.Contains(fmt.Sprint(m), certificate) || strings.Contains(fmt.Sprint(created.Diagnostics), secret) {
		t.Fatal("secret leaked")
	}
	read := resource.ReadResponse{State: created.State}
	svc.Read(ctx, resource.ReadRequest{State: created.State}, &read)
	if read.Diagnostics.HasError() {
		t.Fatal(read.Diagnostics)
	}
	deleted := resource.DeleteResponse{State: read.State}
	svc.Delete(ctx, resource.DeleteRequest{State: read.State}, &deleted)
	if deleted.Diagnostics.HasError() || removes != 1 || present {
		t.Fatalf("delete %v removes=%d", deleted.Diagnostics, removes)
	}
}
func TestCustomDomainPreflightAndReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		absent    bool
	}{
		{"existing", domainResponse, false},
		{"other", `{"statusCode":200,"data":{"customDomain":"other.example.com","httpsPrivateKey":"` + secret + `"}}`, false},
		{"malformed", `{`, false},
		{"server", `{"statusCode":500,"message":"` + secret + `"}`, false},
		{"missingData", `{"statusCode":200,"data":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/get-custom-domain" {
					writes++
				}
				fmt.Fprint(w, tc.raw)
			})
			svc := &CustomDomainResource{client: c}
			ctx := context.Background()
			cr := resource.CreateResponse{State: objectState(t, svc, &CustomDomainModel{})}
			svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, domainModel())}, &cr)
			if !cr.Diagnostics.HasError() || writes != 0 || strings.Contains(fmt.Sprint(cr.Diagnostics), secret) {
				t.Fatalf("preflight %v writes=%d", cr.Diagnostics, writes)
			}
			rd := resource.ReadResponse{State: objectState(t, svc, domainStateModel())}
			svc.Read(ctx, resource.ReadRequest{State: rd.State}, &rd)
			if tc.name == "existing" {
				if rd.Diagnostics.HasError() {
					t.Fatal(rd.Diagnostics)
				}
			} else if !rd.Diagnostics.HasError() || rd.State.Raw.IsNull() || strings.Contains(fmt.Sprint(rd.Diagnostics), secret) {
				t.Fatalf("read %v", rd.Diagnostics)
			}
		})
	}
}
func TestCustomDomainReadAbsentOnlyOn404(t *testing.T) {
	for _, raw := range []string{`{"statusCode":404}`, `{"statusCode":404,"message":"not found"}`} {
		c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, raw) })
		svc := &CustomDomainResource{client: c}
		st := objectState(t, svc, domainStateModel())
		out := resource.ReadResponse{State: st}
		svc.Read(context.Background(), resource.ReadRequest{State: st}, &out)
		if out.Diagnostics.HasError() || !out.State.Raw.IsNull() {
			t.Fatalf("missing %v", out.Diagnostics)
		}
	}
}
func TestCustomDomainUnsafeDeleteAndVerification(t *testing.T) {
	for _, tc := range []struct {
		name, first, mutation, last string
		writes                      int
	}{
		{"replacement", `{"statusCode":200,"data":{"customDomain":"replacement.example.com","httpsPrivateKey":"` + secret + `"}}`, "", "", 0},
		{"missingIdentity", `{"statusCode":200,"data":{}}`, "", "", 0},
		{"getError", `{"statusCode":500}`, "", "", 0},
		{"mutationFalse", domainResponse, `{"statusCode":200,"data":{"success":false}}`, "", 1},
		{"mutationError", domainResponse, `{"statusCode":500}`, "", 1},
		{"stillPresent", domainResponse, `{"statusCode":200,"data":{"success":true}}`, domainResponse, 1},
		{"verifyError", domainResponse, `{"statusCode":200,"data":{"success":true}}`, `{"statusCode":500}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gets, writes := 0, 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-custom-domain" {
					gets++
					if gets == 1 {
						fmt.Fprint(w, tc.first)
					} else {
						fmt.Fprint(w, tc.last)
					}
				} else {
					writes++
					fmt.Fprint(w, tc.mutation)
				}
			})
			svc := &CustomDomainResource{client: c}
			st := objectState(t, svc, domainStateModel())
			out := resource.DeleteResponse{State: st}
			svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
			if !out.Diagnostics.HasError() || writes != tc.writes || strings.Contains(fmt.Sprint(out.Diagnostics), secret) {
				t.Fatalf("delete %v writes=%d", out.Diagnostics, writes)
			}
		})
	}
}
func TestCustomDomainCreateReadbackAndMutationFailures(t *testing.T) {
	for _, tc := range []struct{ name, mutation, readback string }{
		{"mutationError", `{"statusCode":500}`, ""},
		{"wrongCreate", `{"statusCode":200,"data":{"customDomain":"other.example.com"}}`, ""},
		{"readbackMissing", `{"statusCode":200,"data":{"customDomain":"sso.example.com"}}`, `{"statusCode":404}`},
		{"readbackOther", `{"statusCode":200,"data":{"customDomain":"sso.example.com"}}`, `{"statusCode":200,"data":{"customDomain":"other.example.com","httpsPrivateKey":"` + secret + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gets := 0
			c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v3/get-custom-domain" {
					gets++
					if gets == 1 {
						fmt.Fprint(w, `{"statusCode":404}`)
					} else {
						fmt.Fprint(w, tc.readback)
					}
				} else {
					fmt.Fprint(w, tc.mutation)
				}
			})
			svc := &CustomDomainResource{client: c}
			ctx := context.Background()
			out := resource.CreateResponse{State: objectState(t, svc, &CustomDomainModel{})}
			svc.Create(ctx, resource.CreateRequest{Plan: objectPlan(t, svc, domainModel())}, &out)
			var m CustomDomainModel
			d := out.State.Get(ctx, &m)
			if !out.Diagnostics.HasError() || d.HasError() || !m.ID.IsNull() || !strings.Contains(fmt.Sprint(out.Diagnostics), domain) || strings.Contains(fmt.Sprint(out.Diagnostics), secret) {
				t.Fatalf("failure %+v %v", m, out.Diagnostics)
			}
		})
	}
}
func TestCustomDomainDeleteAlreadyAbsentDoesNotMutate(t *testing.T) {
	writes := 0
	c := objectTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/get-custom-domain" {
			writes++
		}
		fmt.Fprint(w, `{"statusCode":404}`)
	})
	svc := &CustomDomainResource{client: c}
	st := objectState(t, svc, domainStateModel())
	out := resource.DeleteResponse{State: st}
	svc.Delete(context.Background(), resource.DeleteRequest{State: st}, &out)
	if out.Diagnostics.HasError() || writes != 0 {
		t.Fatalf("absent delete %v writes=%d", out.Diagnostics, writes)
	}
}

func TestCustomDomainImportAndSchema(t *testing.T) {
	svc := NewCustomDomainResource()
	ctx := context.Background()
	s := resource.SchemaResponse{}
	svc.Schema(ctx, resource.SchemaRequest{}, &s)
	a, ok := s.Schema.Attributes["custom_domain"].(schema.StringAttribute)
	if !ok || !a.IsRequired() || len(a.PlanModifiers) == 0 {
		t.Fatal("domain must be immutable")
	}
	for _, k := range []string{"https_private_key", "https_certificate"} {
		if _, ok := s.Schema.Attributes[k]; ok {
			t.Fatalf("secret in schema %s", k)
		}
	}
	out := resource.ImportStateResponse{State: objectState(t, svc, &CustomDomainModel{})}
	svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: domain}, &out)
	var m CustomDomainModel
	if d := out.State.Get(ctx, &m); d.HasError() || out.Diagnostics.HasError() || m.ID.ValueString() != domain || m.CustomDomain.ValueString() != domain {
		t.Fatalf("import %+v %v", m, out.Diagnostics)
	}
	for _, bad := range []string{"", " ", "HTTPS://bad"} {
		out := resource.ImportStateResponse{State: objectState(t, svc, &CustomDomainModel{})}
		svc.(resource.ResourceWithImportState).ImportState(ctx, resource.ImportStateRequest{ID: bad}, &out)
		if !out.Diagnostics.HasError() {
			t.Errorf("accepted %q", bad)
		}
	}
}
