package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestDataObjectRowLookupByFieldID(t *testing.T) {
	calls := 0
	result := lookupRead(t, NewDataObjectRowDataSource(), map[string]string{"model_id": "model & one", "row_id": "row/one"}, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/metadata/get-row" || r.URL.Query().Encode() != "modelId=model+%26+one&rowId=row%2Fone&showFieldId=true&showRelation=false" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"statusCode":200,"data":{"rowId":"row/one","cellList":[{"fieldId":"f2","value":{"nested":[1,true]}},{"fieldId":"f1","value":{"secret":"PRIVATE"}}]}}`)
	})
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	var got DataObjectRowDataSourceModel
	if diags := result.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	var cells map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.CellsJSON.ValueString()), &cells); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.ModelID.ValueString() != "model & one" || got.RowID.ValueString() != "row/one" || len(cells) != 2 || string(cells["f1"]) != `{"secret":"PRIVATE"}` || string(cells["f2"]) != `{"nested":[1,true]}` {
		t.Fatalf("incorrect field ID mapping or identity: %+v", got)
	}
}

func TestDataObjectRowLookupFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"row mismatch", `{"statusCode":200,"data":{"rowId":"other","cellList":[]}}`, 200},
		{"no row id", `{"statusCode":200,"data":{"cellList":[]}}`, 200},
		{"no cells", `{"statusCode":200,"data":{"rowId":"row"}}`, 200},
		{"duplicate ID", `{"statusCode":200,"data":{"rowId":"row","cellList":[{"fieldId":"f","value":{}},{"fieldId":"f","value":{}}]}}`, 200},
		{"missing field ID", `{"statusCode":200,"data":{"rowId":"row","cellList":[{"value":{}}]}}`, 200},
		{"missing value", `{"statusCode":200,"data":{"rowId":"row","cellList":[{"fieldId":"f"}]}}`, 200},
		{"null value", `{"statusCode":200,"data":{"rowId":"row","cellList":[{"fieldId":"f","value":null}]}}`, 200},
		{"404", `{"statusCode":404,"message":"PRIVATE"}`, 404},
		{"business failure", `{"statusCode":403,"message":"PRIVATE"}`, 200},
		{"server failure", `PRIVATE`, 502},
		{"invalid JSON", `PRIVATE`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := lookupRead(t, NewDataObjectRowDataSource(), map[string]string{"model_id": "model", "row_id": "row"}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			if !result.Diagnostics.HasError() || strings.Contains(fmt.Sprint(result.Diagnostics), "PRIVATE") || !result.State.Raw.IsNull() {
				t.Fatalf("unsafe failure: %v state=%v", result.Diagnostics, result.State.Raw)
			}
		})
	}
}

func TestDataObjectRowLookupRejectsEmptyIdentity(t *testing.T) {
	for _, values := range []map[string]string{{"model_id": "", "row_id": "row"}, {"model_id": "model", "row_id": ""}} {
		calls := 0
		result := lookupRead(t, NewDataObjectRowDataSource(), values, func(http.ResponseWriter, *http.Request) { calls++ })
		if !result.Diagnostics.HasError() || calls != 0 {
			t.Fatalf("expected local validation failure: %v calls=%d", result.Diagnostics, calls)
		}
	}
}

func TestDataObjectRowRegisteredReadOnlyAndSensitive(t *testing.T) {
	for _, factory := range (&AuthingProvider{}).DataSources(context.Background()) {
		d := factory()
		m := datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "authing"}, &m)
		if m.TypeName != "authing_data_object_row" {
			continue
		}
		s := datasource.SchemaResponse{}
		d.Schema(context.Background(), datasource.SchemaRequest{}, &s)
		if !s.Schema.Attributes["cells_json"].IsSensitive() {
			t.Fatal("cells must be marked sensitive")
		}
		return
	}
	t.Fatal("row data source not registered")
}
