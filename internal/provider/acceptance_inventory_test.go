package provider

import (
	"context"
	"encoding/csv"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Keep the sandbox test plan exhaustive as the provider's registry evolves.
// A matrix row documents intent, not evidence that a live API case passed.
func TestAcceptanceMatrixCoversRegisteredResources(t *testing.T) {
	file, err := os.Open("../../docs/acceptance-matrix.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || !reflect.DeepEqual(rows[0], []string{"resource", "dependency", "drift_oracle", "update_mode", "safety_limit", "live_result"}) {
		t.Fatal("acceptance matrix schema changed")
	}
	var documented []string
	seen := make(map[string]bool)
	for _, row := range rows[1:] {
		name := "authing_" + row[0]
		if seen[name] {
			t.Fatalf("duplicate matrix entry: %s", name)
		}
		seen[name] = true
		if row[2] == "" || row[3] == "" || row[4] == "" || row[5] == "" {
			t.Fatalf("incomplete matrix entry: %s", name)
		}
		documented = append(documented, name)
	}
	var registered []string
	for _, newResource := range (&AuthingProvider{}).Resources(context.Background()) {
		meta := resource.MetadataResponse{}
		newResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "authing"}, &meta)
		registered = append(registered, meta.TypeName)
	}
	sort.Strings(documented)
	sort.Strings(registered)
	if !reflect.DeepEqual(registered, documented) {
		t.Errorf("acceptance matrix must enumerate each registered resource exactly once\nregistry: %v\nmatrix: %v", registered, documented)
	}
}
