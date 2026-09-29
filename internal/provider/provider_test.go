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

	if len(resourceFactories) != 19 {
		t.Fatalf("expected 19 registered resources, got %d", len(resourceFactories))
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
		"authing_data_object",
		"authing_data_object_field",
		"authing_data_resource",
		"authing_data_policy",
		"authing_department",
		"authing_department_member",
		"authing_ext_idp",
		"authing_group",
		"authing_group_member",
		"authing_namespace",
		"authing_organization",
		"authing_pipeline_function",
		"authing_post",
		"authing_resource",
		"authing_role",
		"authing_role_assignment",
		"authing_user",
		"authing_webhook",
	})
}

func TestRegisteredDataSourcesExposeMetadataAndSchema(t *testing.T) {
	provider := &AuthingProvider{}
	dataSourceFactories := provider.DataSources(context.Background())

	if len(dataSourceFactories) != 10 {
		t.Fatalf("expected 10 registered data sources, got %d", len(dataSourceFactories))
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
		"authing_data_resource",
		"authing_department",
		"authing_group",
		"authing_namespace",
		"authing_organization",
		"authing_resource",
		"authing_role",
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
