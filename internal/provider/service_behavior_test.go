package provider

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"terraform-provider-authing/internal/authingapi"
)

func TestRegisteredResourceCreateCallsAuthing(t *testing.T) {
	ctx := context.Background()
	var apiCalls atomic.Int64
	server := newServiceTestServer(&apiCalls)
	defer server.Close()

	for _, factory := range (&AuthingProvider{}).Resources(ctx) {
		service := factory()
		metadata := resource.MetadataResponse{}
		service.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "authing"}, &metadata)
		// These require correlated or type-specific API responses; dedicated httptest lifecycles cover them.
		if metadata.TypeName == "authing_data_object_field" || metadata.TypeName == "authing_data_resource" || metadata.TypeName == "authing_data_policy_assignment" {
			continue
		}

		t.Run(metadata.TypeName, func(t *testing.T) {
			schema := resource.SchemaResponse{}
			service.Schema(ctx, resource.SchemaRequest{}, &schema)
			client := newServiceTestClient(t, server.URL, metadata.TypeName)
			configure := resource.ConfigureResponse{}
			configurable, ok := service.(interface {
				Configure(context.Context, resource.ConfigureRequest, *resource.ConfigureResponse)
			})
			if !ok {
				t.Fatal("resource does not implement Configure")
			}
			configurable.Configure(ctx, resource.ConfigureRequest{ProviderData: client}, &configure)
			if configure.Diagnostics.HasError() {
				t.Fatalf("configure resource: %v", configure.Diagnostics)
			}

			plan := tfsdk.Plan{Schema: schema.Schema, Raw: serviceTestValue(schema.Schema.Type().TerraformType(ctx), serviceRequiredAttributes(schema.Schema.GetAttributes()))}
			if metadata.TypeName == "authing_data_object" {
				// Creation cannot set this update-only API field.
				if diags := plan.SetAttribute(ctx, path.Root("show_field_key"), ""); diags.HasError() {
					t.Fatal(diags)
				}
			}
			response := resource.CreateResponse{State: tfsdk.State{Schema: schema.Schema}}
			before := apiCalls.Load()
			service.Create(ctx, resource.CreateRequest{Plan: plan}, &response)
			if response.Diagnostics.HasError() {
				t.Fatalf("create service resource: %v", response.Diagnostics)
			}
			if apiCalls.Load() != before+1 {
				t.Fatalf("expected one Authing API call, got %d", apiCalls.Load()-before)
			}
			if response.State.Raw.IsNull() {
				t.Fatal("create did not write resource state")
			}
		})
	}
}

func TestRegisteredDataSourceReadCallsAuthing(t *testing.T) {
	ctx := context.Background()
	var apiCalls atomic.Int64
	server := newServiceTestServer(&apiCalls)
	defer server.Close()

	for _, factory := range (&AuthingProvider{}).DataSources(ctx) {
		service := factory()
		metadata := datasource.MetadataResponse{}
		service.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "authing"}, &metadata)
		// The generic fixture has no valid data-resource struct; dedicated httptest covers it.
		if metadata.TypeName == "authing_data_resource" {
			continue
		}

		t.Run(metadata.TypeName, func(t *testing.T) {
			schema := datasource.SchemaResponse{}
			service.Schema(ctx, datasource.SchemaRequest{}, &schema)
			client := newServiceTestClient(t, server.URL, metadata.TypeName)
			configure := datasource.ConfigureResponse{}
			configurable, ok := service.(interface {
				Configure(context.Context, datasource.ConfigureRequest, *datasource.ConfigureResponse)
			})
			if !ok {
				t.Fatal("data source does not implement Configure")
			}
			configurable.Configure(ctx, datasource.ConfigureRequest{ProviderData: client}, &configure)
			if configure.Diagnostics.HasError() {
				t.Fatalf("configure data source: %v", configure.Diagnostics)
			}

			request := datasource.ReadRequest{Config: tfsdk.Config{
				Schema: schema.Schema,
				Raw:    serviceTestValue(schema.Schema.Type().TerraformType(ctx), serviceRequiredAttributes(schema.Schema.GetAttributes())),
			}}
			if metadata.TypeName == "authing_application_subject_auth" {
				objectType := schema.Schema.Type().TerraformType(ctx).(tftypes.Object)
				attributes := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
				for name, typ := range objectType.AttributeTypes {
					value, ok := map[string]string{"target_id": "test-value", "target_type": "USER", "app_id": "test-app"}[name]
					if ok {
						attributes[name] = tftypes.NewValue(typ, value)
					} else {
						attributes[name] = tftypes.NewValue(typ, nil)
					}
				}
				request.Config.Raw = tftypes.NewValue(objectType, attributes)
			}
			response := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
			before := apiCalls.Load()
			service.Read(ctx, request, &response)
			if response.Diagnostics.HasError() {
				t.Fatalf("read service data source: %v", response.Diagnostics)
			}
			if apiCalls.Load() != before+1 {
				t.Fatalf("expected one Authing API call, got %d", apiCalls.Load()-before)
			}
			if response.State.Raw.IsNull() {
				t.Fatal("read did not write data source state")
			}
		})
	}
}

func newServiceTestServer(apiCalls *atomic.Int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/get-management-token" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"access_token":"service-test-token","expires_in":3600}}`)
			return
		}

		apiCalls.Add(1)
		if r.URL.Path == "/api/v3/device-status" {
			fmt.Fprint(w, `{"statusCode":200,"data":{"status":"activated"}}`)
			return
		}
		fmt.Fprint(w, `{"statusCode":200,"message":"ok","data":{"id":"test-id","userId":"test-user","username":"test-user","email":"test@example.com","phone":"10000000000","nickname":"Test User","externalId":"external-user","gender":"U","emailVerified":false,"phoneVerified":false,"code":"test-code","name":"Test Name","description":"Test description","organizationCode":"test-organization","organizationName":"Test Organization","departmentId":"test-department","departmentCode":"test-department","postId":"test-post","namespace":"test-namespace","namespaceCode":"test-namespace","roleCode":"test-role","resourceCode":"test-resource","resourceId":"test-resource","policyId":"test-policy","appId":"test-app","appName":"Test Application","reqTargetId":"test-value","reqTargetName":"Test Subject","reqTargetType":"USER","targetType":"USER","targetName":"Test Subject","authType":"SUBJECT","webhookId":"test-webhook","funcId":"test-function","functionId":"test-function","list":[]}}`)
	}))
}

func newServiceTestClient(t *testing.T, host, serviceName string) *authingapi.Client {
	t.Helper()
	client, err := authingapi.NewClient(authingapi.Options{
		AccessKeyID:     "service-test-" + serviceName,
		AccessKeySecret: "test-secret",
		Host:            host,
	})
	if err != nil {
		t.Fatalf("create management client: %v", err)
	}
	return client
}

func serviceRequiredAttributes[T interface{ IsRequired() bool }](attributes map[string]T) map[string]bool {
	required := make(map[string]bool, len(attributes))
	for name, attribute := range attributes {
		required[name] = attribute.IsRequired()
	}
	return required
}

func serviceTestValue(valueType tftypes.Type, requiredAttributes map[string]bool) tftypes.Value {
	objectType, ok := valueType.(tftypes.Object)
	if !ok {
		panic("service schema root is not an object")
	}
	values := make(map[string]tftypes.Value, len(requiredAttributes))
	for name := range objectType.AttributeTypes {
		attributeType := objectType.AttributeTypes[name]
		if requiredAttributes[name] {
			values[name] = serviceTestRequiredValue(attributeType)
		} else {
			values[name] = tftypes.NewValue(attributeType, nil)
		}
	}
	return tftypes.NewValue(objectType, values)
}

func serviceTestRequiredValue(valueType tftypes.Type) tftypes.Value {
	switch {
	case valueType.Equal(tftypes.String):
		return tftypes.NewValue(valueType, "test-value")
	case valueType.Equal(tftypes.Bool):
		return tftypes.NewValue(valueType, true)
	case valueType.Equal(tftypes.Number):
		return tftypes.NewValue(valueType, big.NewFloat(1))
	}
	switch typedType := valueType.(type) {
	case tftypes.List:
		return tftypes.NewValue(valueType, []tftypes.Value{})
	case tftypes.Set:
		return tftypes.NewValue(valueType, []tftypes.Value{})
	case tftypes.Map:
		return tftypes.NewValue(valueType, map[string]tftypes.Value{})
	case tftypes.Object:
		values := make(map[string]tftypes.Value, len(typedType.AttributeTypes))
		for name, nestedType := range typedType.AttributeTypes {
			values[name] = serviceTestRequiredValue(nestedType)
		}
		return tftypes.NewValue(valueType, values)
	case tftypes.Tuple:
		values := make([]tftypes.Value, len(typedType.ElementTypes))
		for index, elementType := range typedType.ElementTypes {
			values[index] = serviceTestRequiredValue(elementType)
		}
		return tftypes.NewValue(valueType, values)
	default:
		return tftypes.NewValue(valueType, nil)
	}
}
