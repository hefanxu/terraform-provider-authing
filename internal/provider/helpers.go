package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// AuthingAPIResponse is an interface to check standard Authing responses
type AuthingAPIResponse interface {
	GetStatusCode() int
	GetMessage() string
}

// CheckResponseHelper verifies response error states
func CheckAPIResponse(statusCode int, apiCode int, message string, diags *diag.Diagnostics, actionDesc string) bool {
	if statusCode != 200 || (apiCode != 0 && apiCode != 200 && apiCode != 20001) {
		diags.AddError(
			fmt.Sprintf("Authing API Error during %s", actionDesc),
			fmt.Sprintf("HTTP Status: %d, API Code: %d, Message: %s", statusCode, apiCode, message),
		)
		return false
	}
	return true
}

func StringValOrNil(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func BoolValOrNil(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func IntValOrNil(i *int) int64 {
	if i == nil {
		return 0
	}
	return int64(*i)
}
