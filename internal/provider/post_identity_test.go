package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
)

func TestPostIdentityHasReplacementAndStableIDPlan(t *testing.T) {
	var out resource.SchemaResponse
	(&PostResource{}).Schema(context.Background(), resource.SchemaRequest{}, &out)
	code, ok := out.Schema.Attributes["code"].(schema.StringAttribute)
	if !ok || len(code.PlanModifiers) != 1 || reflect.TypeOf(code.PlanModifiers[0]) != reflect.TypeOf(stringplanmodifier.RequiresReplace()) {
		t.Fatal("code change can mutate identity in place")
	}
	id, ok := out.Schema.Attributes["id"].(schema.StringAttribute)
	if !ok || len(id.PlanModifiers) == 0 {
		t.Fatal("post update loses computed ID")
	}
}
