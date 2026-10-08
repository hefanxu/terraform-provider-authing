package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terraform-provider-authing/internal/authingapi"
)

var _ datasource.DataSource = (*DataResourceExtensionFieldDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*DataResourceExtensionFieldDataSource)(nil)

// Extension definitions are intentionally read-only. The parent update-data-resource
// API does not document whether omitted extendFieldList entries survive a write.
type DataResourceExtensionFieldDataSource struct{ client *authingapi.Client }
type DataResourceExtensionFieldDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	NamespaceCode types.String `tfsdk:"namespace_code"`
	ResourceCode  types.String `tfsdk:"resource_code"`
	Key           types.String `tfsdk:"key"`
	ValueType     types.String `tfsdk:"value_type"`
	Label         types.String `tfsdk:"label"`
	Description   types.String `tfsdk:"description"`
}

func NewDataResourceExtensionFieldDataSource() datasource.DataSource {
	return &DataResourceExtensionFieldDataSource{}
}
func (d *DataResourceExtensionFieldDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_resource_extension_field"
}
func (d *DataResourceExtensionFieldDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read one Authing data-resource extension field by namespace, resource, and exact key. Read-only: update-data-resource does not guarantee preservation of omitted extension definitions, so writes are not exposed. Does not manage per-node extendFieldValue or SELECT options.", Attributes: map[string]schema.Attribute{
		"id":             schema.StringAttribute{Computed: true},
		"namespace_code": schema.StringAttribute{Required: true},
		"resource_code":  schema.StringAttribute{Required: true},
		"key":            schema.StringAttribute{Required: true},
		"value_type":     schema.StringAttribute{Computed: true},
		"label":          schema.StringAttribute{Computed: true},
		"description":    schema.StringAttribute{Computed: true},
	}}
}
func (d *DataResourceExtensionFieldDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*authingapi.Client)
	if !ok {
		resp.Diagnostics.AddError("Invalid client", "Expected *authingapi.Client")
		return
	}
	d.client = c
}
func rejectDuplicateExtensionProperties(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid extension property")
			}
			if seen[name] {
				return fmt.Errorf("duplicate extension property %q", name)
			}
			seen[name] = true
			if err := rejectDuplicateExtensionProperties(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if err := rejectDuplicateExtensionProperties(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		return errors.New("invalid extension JSON delimiter")
	}
}
func strictExtensionJSON(raw []byte, target any) error {
	dup := json.NewDecoder(bytes.NewReader(raw))
	if err := rejectDuplicateExtensionProperties(dup); err != nil {
		return err
	}
	var tail any
	if err := dup.Decode(&tail); !errors.Is(err, io.EOF) {
		return errors.New("invalid trailing extension JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

type extensionField struct {
	Key         *string         `json:"key"`
	ValueType   *string         `json:"valueType"`
	Label       *string         `json:"label"`
	Description *string         `json:"description"`
	Config      json.RawMessage `json:"config"`
}

func validateExtensionField(raw json.RawMessage) (extensionField, error) {
	var f extensionField
	if err := strictExtensionJSON(raw, &f); err != nil {
		return f, err
	}
	if f.Key == nil || *f.Key == "" || f.Label == nil || *f.Label == "" || f.ValueType == nil {
		return f, errors.New("incomplete extension field")
	}
	if *f.ValueType != "STRING" && *f.ValueType != "SELECT" {
		return f, errors.New("unsupported extension valueType")
	}
	if len(f.Config) > 0 && string(f.Config) != "null" {
		var cfg struct {
			Options []struct {
				Value *string `json:"value"`
				Label *string `json:"label"`
			} `json:"options"`
		}
		if err := strictExtensionJSON(f.Config, &cfg); err != nil {
			return f, err
		}
		if cfg.Options == nil {
			return f, errors.New("extension config requires options")
		}
		for _, o := range cfg.Options {
			if o.Value == nil || *o.Value == "" {
				return f, errors.New("extension option requires a value")
			}
		}
	} else if *f.ValueType == "SELECT" {
		return f, errors.New("SELECT extension requires config")
	}
	return f, nil
}
func (d *DataResourceExtensionFieldDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m DataResourceExtensionFieldDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.NamespaceCode.IsNull() || m.NamespaceCode.IsUnknown() || m.NamespaceCode.ValueString() == "" || m.ResourceCode.IsNull() || m.ResourceCode.IsUnknown() || m.ResourceCode.ValueString() == "" || m.Key.IsNull() || m.Key.IsUnknown() || m.Key.ValueString() == "" {
		resp.Diagnostics.AddError("Invalid extension field identity", "namespace_code, resource_code, and key must be known and nonempty")
		return
	}
	if d.client == nil {
		resp.Diagnostics.AddError("Extension field lookup failed", "Client is not configured")
		return
	}
	start := 1
	seenIndexes := map[int]bool{}
	keys := map[string]bool{}
	var matched *extensionField
	for {
		if seenIndexes[start] {
			resp.Diagnostics.AddError("Invalid extension field response", "Pagination repeated an index")
			return
		}
		seenIndexes[start] = true
		raw, err := d.client.SendHttpRequestContext(ctx, "/api/v3/list-dnef", http.MethodGet, map[string]string{"namespaceCode": m.NamespaceCode.ValueString(), "resourceCode": m.ResourceCode.ValueString(), "startIndex": strconv.Itoa(start), "maxSize": "50"})
		if err != nil {
			resp.Diagnostics.AddError("Extension field lookup failed", "Authing request failed")
			return
		}
		var envelope struct {
			StatusCode *int            `json:"statusCode"`
			Data       json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.StatusCode == nil || *envelope.StatusCode != 200 {
			resp.Diagnostics.AddError("Extension field lookup failed", "Authing returned an unsuccessful or malformed response")
			return
		}
		var page struct {
			NextStartIndex *int              `json:"nextStartIndex"`
			Truncated      *bool             `json:"truncated"`
			List           []json.RawMessage `json:"list"`
		}
		if strictExtensionJSON(envelope.Data, &page) != nil || page.NextStartIndex == nil || page.Truncated == nil || page.List == nil || len(page.List) > 50 || (*page.Truncated && (len(page.List) == 0 || *page.NextStartIndex <= start)) || (!*page.Truncated && *page.NextStartIndex != -1) {
			resp.Diagnostics.AddError("Invalid extension field response", "Incomplete or inconsistent pagination")
			return
		}
		for _, item := range page.List {
			f, e := validateExtensionField(item)
			if e != nil {
				resp.Diagnostics.AddError("Invalid extension field response", fmt.Sprintf("Invalid extension definition: %s", e))
				return
			}
			if keys[*f.Key] {
				resp.Diagnostics.AddError("Ambiguous extension field response", "Duplicate extension key")
				return
			}
			keys[*f.Key] = true
			if *f.Key == m.Key.ValueString() {
				matched = &f
			}
		}
		if !*page.Truncated {
			break
		}
		start = *page.NextStartIndex
	}
	if matched == nil {
		resp.Diagnostics.AddError("Extension field not found", "No definition matched the exact key")
		return
	}
	m.ID = types.StringValue(func() string {
		b, _ := json.Marshal([]string{m.NamespaceCode.ValueString(), m.ResourceCode.ValueString(), m.Key.ValueString()})
		return string(b)
	}())
	m.ValueType = types.StringValue(*matched.ValueType)
	m.Label = types.StringValue(*matched.Label)
	m.Description = customFieldString(matched.Description)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
