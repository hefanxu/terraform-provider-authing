package provider

import (
	"context"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestRegisteredResourcesExposeMetadataAndSchema(t *testing.T) {
	provider := &AuthingProvider{}
	resourceFactories := provider.Resources(context.Background())

	if len(resourceFactories) != 27 {
		t.Fatalf("expected 27 registered resources, got %d", len(resourceFactories))
	}

	resourceNames := make([]string, 0, len(resourceFactories))
	for _, factory := range resourceFactories {
		instance := factory()
		metadata := resource.MetadataResponse{}
		instance.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "authing"}, &metadata)

		if metadata.TypeName == "" {
			t.Fatal("resource metadata returned an empty type name")
		}
		resourceNames = append(resourceNames, metadata.TypeName)

		t.Run(metadata.TypeName, func(t *testing.T) {
			schema := resource.SchemaResponse{}
			instance.Schema(context.Background(), resource.SchemaRequest{}, &schema)
			if len(schema.Schema.Attributes) == 0 {
				t.Error("resource schema has no attributes")
			}
		})
	}

	assertServiceNames(t, resourceNames, []string{
		"authing_application",
		"authing_auth_flow_function",
		"authing_data_object",
		"authing_data_object_field",
		"authing_data_resource",
		"authing_data_policy",
		"authing_data_policy_assignment",
		"authing_department",
		"authing_department_member",
		"authing_ext_idp",
		"authing_group",
		"authing_group_member",
		"authing_invitation_policy",
		"authing_invitation_roster",
		"authing_namespace",
		"authing_organization",
		"authing_pipeline_function",
		"authing_post",
		"authing_public_account",
		"authing_resource",
		"authing_role",
		"authing_role_assignment",
		"authing_tenant",
		"authing_tenant_admin",
		"authing_tenant_membership",
		"authing_user",
		"authing_webhook",
	})
}

func TestRegisteredDataSourcesExposeMetadataAndSchema(t *testing.T) {
	provider := &AuthingProvider{}
	dataSourceFactories := provider.DataSources(context.Background())

	if len(dataSourceFactories) != 15 {
		t.Fatalf("expected 15 registered data sources, got %d", len(dataSourceFactories))
	}

	dataSourceNames := make([]string, 0, len(dataSourceFactories))
	for _, factory := range dataSourceFactories {
		instance := factory()
		metadata := datasource.MetadataResponse{}
		instance.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &metadata)

		if metadata.TypeName == "" {
			t.Fatal("data source metadata returned an empty type name")
		}
		dataSourceNames = append(dataSourceNames, metadata.TypeName)

		t.Run(metadata.TypeName, func(t *testing.T) {
			schema := datasource.SchemaResponse{}
			instance.Schema(context.Background(), datasource.SchemaRequest{}, &schema)
			if len(schema.Schema.Attributes) == 0 {
				t.Error("data source schema has no attributes")
			}
		})
	}

	assertServiceNames(t, dataSourceNames, []string{
		"authing_application",
		"authing_application_subject_auth",
		"authing_data_resource",
		"authing_department",
		"authing_device_status",
		"authing_ext_idp_connection",
		"authing_group",
		"authing_namespace",
		"authing_organization",
		"authing_resource",
		"authing_public_account",
		"authing_role",
		"authing_tenant",
		"authing_tenant_custom_field",
		"authing_user",
		"authing_users",
	})
}

func assertServiceNames(t *testing.T, actual, expected []string) {
	t.Helper()
	sort.Strings(actual)
	sort.Strings(expected)

	if len(actual) != len(expected) {
		t.Fatalf("expected service names %v, got %v", expected, actual)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("expected service names %v, got %v", expected, actual)
		}
	}
}
