package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"terraform-provider-authing/internal/authingapi"
)

// A closed set of labels is the entire observable diagnostic surface.
type namespaceRecoveryStage string

const (
	recoveryGet           namespaceRecoveryStage = "get"
	recoveryRoles         namespaceRecoveryStage = "roles"
	recoveryResources     namespaceRecoveryStage = "resources"
	recoveryDataResources namespaceRecoveryStage = "data-resources"
	recoveryPolicies      namespaceRecoveryStage = "policies"
)

type namespaceRecoveryCause string

const (
	recoveryTransport           namespaceRecoveryCause = "transport"
	recoveryInvalidEnvelope     namespaceRecoveryCause = "invalid-envelope"
	recoveryBusiness4xx         namespaceRecoveryCause = "business-4xx"
	recoveryBusiness5xx         namespaceRecoveryCause = "business-5xx"
	recoveryIncompleteInventory namespaceRecoveryCause = "incomplete-inventory"
)

// Only structural labels, never values from the response, enter diagnostics.
type namespaceResourceShape string

const (
	resourceShapeUnavailable              namespaceResourceShape = "unavailable"
	resourceShapeMalformedJSON            namespaceResourceShape = "malformed-json"
	resourceShapeDataNotObject            namespaceResourceShape = "data-not-object"
	resourceShapeMissingNestedStatus      namespaceResourceShape = "missing-nested-status"
	resourceShapeMissingNestedStatusEmpty namespaceResourceShape = "missing-nested-status-empty-page"
	resourceShapeNestedStatusInvalid      namespaceResourceShape = "nested-status-invalid"
	resourceShapeMissingTotal             namespaceResourceShape = "missing-total"
	resourceShapeTotalInvalid             namespaceResourceShape = "total-invalid"
	resourceShapeMissingList              namespaceResourceShape = "missing-list"
	resourceShapeListNull                 namespaceResourceShape = "list-null"
	resourceShapeListNotArray             namespaceResourceShape = "list-not-array"
)

type namespaceRecoveryDiagnostic struct {
	Status string
	Stage  namespaceRecoveryStage
	Cause  namespaceRecoveryCause
	Shape  namespaceResourceShape
}

func recoveryUnknown(stage namespaceRecoveryStage, cause namespaceRecoveryCause) namespaceRecoveryDiagnostic {
	return namespaceRecoveryDiagnostic{Status: "unknown", Stage: stage, Cause: cause}
}

func recoveryResourceInvalid(shape namespaceResourceShape) namespaceRecoveryDiagnostic {
	d := recoveryUnknown(recoveryResources, recoveryInvalidEnvelope)
	d.Shape = shape
	return d
}

// Classify only JSON structure, never log raw fields, messages or elements.
func classifyResourceShape(data json.RawMessage) namespaceResourceShape {
	if len(data) == 0 {
		return resourceShapeDataNotObject
	}
	if !json.Valid(data) {
		return resourceShapeMalformedJSON
	}
	if data[0] != '{' {
		return resourceShapeDataNotObject
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return resourceShapeMalformedJSON
	}
	status, exists := fields["statusCode"]
	if !exists {
		var page struct {
			TotalCount *int              `json:"totalCount"`
			List       []json.RawMessage `json:"list"`
		}
		if len(fields) == 2 && json.Unmarshal(data, &page) == nil && page.TotalCount != nil && *page.TotalCount == 0 && page.List != nil && len(page.List) == 0 {
			return resourceShapeMissingNestedStatusEmpty
		}
		return resourceShapeMissingNestedStatus
	}
	var code int
	if json.Unmarshal(status, &code) != nil || string(status) == "null" {
		return resourceShapeNestedStatusInvalid
	}
	total, exists := fields["totalCount"]
	if !exists {
		return resourceShapeMissingTotal
	}
	var count int
	if json.Unmarshal(total, &count) != nil || string(total) == "null" || count < 0 {
		return resourceShapeTotalInvalid
	}
	list, exists := fields["list"]
	if !exists {
		return resourceShapeMissingList
	}
	if string(list) == "null" {
		return resourceShapeListNull
	}
	if len(list) == 0 || list[0] != '[' {
		return resourceShapeListNotArray
	}
	return resourceShapeUnavailable
}

// No error string, response body, numeric server code, or server message escapes.
func recoveryErrorCause(err error) namespaceRecoveryCause {
	var httpError *authingapi.HTTPStatusError
	if errors.As(err, &httpError) {
		if httpError.StatusCode >= 400 && httpError.StatusCode < 500 {
			return recoveryBusiness4xx
		}
		if httpError.StatusCode >= 500 && httpError.StatusCode < 600 {
			return recoveryBusiness5xx
		}
	}
	if errors.Is(err, authingapi.ErrInvalidResponse) {
		return recoveryInvalidEnvelope
	}
	return recoveryTransport
}

func recoveryBusinessCause(code int) namespaceRecoveryCause {
	if code >= 400 && code < 500 {
		return recoveryBusiness4xx
	}
	if code >= 500 && code < 600 {
		return recoveryBusiness5xx
	}
	return recoveryInvalidEnvelope
}

// OpenAPI: each response has an outer statusCode/data; only list-resources
// additionally has a statusCode inside data (ResourcePagingDto).
func recoveryEnvelope(body []byte) (int, json.RawMessage, namespaceRecoveryCause) {
	var response struct {
		StatusCode *int            `json:"statusCode"`
		Data       json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.StatusCode == nil {
		return 0, nil, recoveryInvalidEnvelope
	}
	if *response.StatusCode != http.StatusOK {
		return *response.StatusCode, nil, recoveryBusinessCause(*response.StatusCode)
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return 0, nil, recoveryInvalidEnvelope
	}
	return http.StatusOK, response.Data, ""
}

func probeNamespaceRecoveryDiagnostic(client *authingapi.Client, code string) namespaceRecoveryDiagnostic {
	if client == nil || !sandboxCode.MatchString(code) {
		return recoveryUnknown(recoveryGet, recoveryInvalidEnvelope)
	}
	body, err := client.SendHttpRequest("/api/v3/get-permission-namespace", http.MethodGet, map[string]string{"code": code})
	if err != nil {
		return recoveryUnknown(recoveryGet, recoveryErrorCause(err))
	}
	status, data, cause := recoveryEnvelope(body)
	if status == http.StatusNotFound {
		return namespaceRecoveryDiagnostic{Status: "absent"}
	}
	if cause != "" {
		return recoveryUnknown(recoveryGet, cause)
	}
	var object struct {
		Code        *string `json:"code"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if json.Unmarshal(data, &object) != nil || object.Code == nil || object.Name == nil || object.Description == nil {
		return recoveryUnknown(recoveryGet, recoveryInvalidEnvelope)
	}
	if *object.Code != code || (*object.Name != code && *object.Name != code+"-drift") || *object.Description != "hermesacc ownership "+code {
		return namespaceRecoveryDiagnostic{Status: "foreign"}
	}
	entries := []struct {
		stage  namespaceRecoveryStage
		path   string
		query  map[string]any
		nested bool
	}{
		{recoveryRoles, "/api/v3/list-permission-namespace-roles", map[string]any{"code": code, "page": 1, "limit": 1}, false},
		{recoveryResources, "/api/v3/list-resources", map[string]any{"namespace": code, "page": 1, "limit": 1}, true},
		{recoveryDataResources, "/api/v3/list-data-resources", map[string]any{"namespaceCodes": []string{code}, "page": 1, "limit": 1}, false},
		{recoveryPolicies, "/api/v3/list-data-policies", map[string]any{"page": 1, "limit": 1}, false},
	}
	nonempty := false
	for _, entry := range entries {
		body, err = client.SendHttpRequest(entry.path, http.MethodGet, entry.query)
		if err != nil {
			cause := recoveryErrorCause(err)
			if entry.stage == recoveryResources && cause == recoveryInvalidEnvelope {
				// The client withholds malformed outer envelopes; no body is available.
				return recoveryResourceInvalid(resourceShapeUnavailable)
			}
			return recoveryUnknown(entry.stage, cause)
		}
		_, data, cause = recoveryEnvelope(body)
		if cause != "" {
			if entry.stage == recoveryResources && cause == recoveryInvalidEnvelope {
				return recoveryResourceInvalid(classifyResourceShape(data))
			}
			return recoveryUnknown(entry.stage, cause)
		}
		var page struct {
			StatusCode *int            `json:"statusCode"`
			TotalCount *int            `json:"totalCount"`
			List       json.RawMessage `json:"list"`
		}
		if json.Unmarshal(data, &page) != nil {
			if entry.stage == recoveryResources {
				return recoveryResourceInvalid(classifyResourceShape(data))
			}
			return recoveryUnknown(entry.stage, recoveryInvalidEnvelope)
		}
		if entry.nested {
			if page.StatusCode == nil {
				// The live service returns a complete empty resource page without
				// the nested statusCode required by OpenAPI. Accept only that
				// exact structural form under a successful outer envelope.
				if classifyResourceShape(data) != resourceShapeMissingNestedStatusEmpty {
					return recoveryResourceInvalid(classifyResourceShape(data))
				}
			}
			if page.StatusCode != nil && *page.StatusCode != http.StatusOK {
				return recoveryUnknown(entry.stage, recoveryBusinessCause(*page.StatusCode))
			}
		}
		var list []json.RawMessage
		if page.TotalCount == nil || *page.TotalCount < 0 || len(page.List) == 0 || page.List[0] != '[' || json.Unmarshal(page.List, &list) != nil {
			if entry.stage == recoveryResources {
				return recoveryResourceInvalid(classifyResourceShape(data))
			}
			return recoveryUnknown(entry.stage, recoveryInvalidEnvelope)
		}
		if (*page.TotalCount == 0 && len(list) != 0) || (*page.TotalCount > 0 && len(list) != 1) {
			return recoveryUnknown(entry.stage, recoveryIncompleteInventory)
		}
		if *page.TotalCount > 0 {
			nonempty = true
		}
	}
	if nonempty {
		return namespaceRecoveryDiagnostic{Status: "owned_nonempty"}
	}
	return namespaceRecoveryDiagnostic{Status: "owned_empty"}
}

func formatNamespaceRecoveryLog(d namespaceRecoveryDiagnostic, code string) string {
	if !sandboxCode.MatchString(code) {
		code = "invalid"
	}
	switch d.Status {
	case "absent", "foreign", "owned_empty", "owned_nonempty":
		return fmt.Sprintf("namespace recovery status=%s code=%s", d.Status, code)
	case "unknown":
		// Only closed enum values may be interpolated, even if a future caller
		// accidentally constructs a diagnostic with untrusted fields.
	default:
		d = recoveryUnknown(recoveryGet, recoveryInvalidEnvelope)
	}
	switch d.Stage {
	case recoveryGet, recoveryRoles, recoveryResources, recoveryDataResources, recoveryPolicies:
	default:
		d.Stage = recoveryGet
	}
	switch d.Cause {
	case recoveryTransport, recoveryInvalidEnvelope, recoveryBusiness4xx, recoveryBusiness5xx, recoveryIncompleteInventory:
	default:
		d.Cause = recoveryInvalidEnvelope
	}
	if d.Stage == recoveryResources && d.Cause == recoveryInvalidEnvelope {
		switch d.Shape {
		case resourceShapeMalformedJSON, resourceShapeDataNotObject, resourceShapeMissingNestedStatus, resourceShapeMissingNestedStatusEmpty,
			resourceShapeNestedStatusInvalid, resourceShapeMissingTotal, resourceShapeTotalInvalid,
			resourceShapeMissingList, resourceShapeListNull, resourceShapeListNotArray:
		default:
			d.Shape = resourceShapeUnavailable
		}
		return fmt.Sprintf("namespace recovery status=unknown stage=%s cause=%s shape=%s code=%s", d.Stage, d.Cause, d.Shape, code)
	}
	return fmt.Sprintf("namespace recovery status=unknown stage=%s cause=%s code=%s", d.Stage, d.Cause, code)
}
