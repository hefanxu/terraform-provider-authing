package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Authing/authing-golang-sdk/v3/management"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestGroupResourceCreate(t *testing.T) {
	ctx := context.Background()
	var createRequest map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/get-management-token":
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"test-token","expires_in":3600}}`)
		case "/api/v3/create-group":
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Errorf("decode create request: %v", err)
			}
			fmt.Fprint(w, `{"statusCode":200,"data":{"code":"engineering","name":"Engineering","description":"Platform team"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := management.NewManagementClient(&management.ManagementClientOptions{
		AccessKeyId:     "group-resource-create-test",
		AccessKeySecret: "test-secret",
		Host:            server.URL,
	})
	if err != nil {
		t.Fatalf("create management client: %v", err)
	}

	service := &GroupResource{client: client}
	schemaResponse := resource.SchemaResponse{}
	service.Schema(ctx, resource.SchemaRequest{}, &schemaResponse)
	plan := tfsdk.Plan{Schema: schemaResponse.Schema}
	planDiags := plan.Set(ctx, &GroupModel{
		Code:        types.StringValue("engineering"),
		Name:        types.StringValue("Engineering"),
		Description: types.StringValue("Platform team"),
	})
	if planDiags.HasError() {
		t.Fatalf("build plan: %v", planDiags)
	}

	response := resource.CreateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	service.Create(ctx, resource.CreateRequest{Plan: plan}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("create group returned diagnostics: %v", response.Diagnostics)
	}
	if createRequest["code"] != "engineering" || createRequest["name"] != "Engineering" || createRequest["description"] != "Platform team" {
		t.Fatalf("unexpected Authing request: %#v", createRequest)
	}

	var state GroupModel
	stateDiags := response.State.Get(ctx, &state)
	if stateDiags.HasError() {
		t.Fatalf("read resource state: %v", stateDiags)
	}
	if state.ID.ValueString() != "engineering" || state.Code.ValueString() != "engineering" || state.Name.ValueString() != "Engineering" {
		t.Fatalf("unexpected resource state: %#v", state)
	}
}
