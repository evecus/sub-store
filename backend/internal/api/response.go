package api

import (
	"net/http"
	"strings"
)

// APIError represents a structured API error.
type APIError struct {
	Code    string `json:"code"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func success(c *Context, data interface{}, statusCode ...int) {
	code := http.StatusOK
	if len(statusCode) > 0 {
		code = statusCode[0]
	}
	c.JSON(code, map[string]interface{}{
		"status": "success",
		"data":   data,
	})
}

func successNoData(c *Context) {
	c.JSON(http.StatusOK, map[string]interface{}{"status": "success", "data": nil})
}

func failed(c *Context, err APIError, statusCode ...int) {
	code := http.StatusInternalServerError
	if len(statusCode) > 0 {
		code = statusCode[0]
	}
	details := err.Details
	if strings.HasPrefix(c.FullPath(), "/share/") {
		details = "详情请查看日志"
	}
	c.JSON(code, map[string]interface{}{
		"status": "failed",
		"error": map[string]interface{}{
			"code":    err.Code,
			"type":    err.Type,
			"message": err.Message,
			"details": details,
		},
	})
}

func errNotFound(name, resource string) APIError {
	return APIError{Code: "RESOURCE_NOT_FOUND", Type: "ResourceNotFoundError",
		Message: resource + " " + name + " does not exist"}
}
func errDuplicate(name string) APIError {
	return APIError{Code: "DUPLICATE_KEY", Type: "RequestInvalidError",
		Message: name + " already exists"}
}
func errInvalidName(name string) APIError {
	return APIError{Code: "INVALID_NAME", Type: "RequestInvalidError",
		Message: name + " is invalid (cannot contain '/')"}
}
func errInternal(msg, details string) APIError {
	return APIError{Code: "INTERNAL_SERVER_ERROR", Type: "InternalServerError",
		Message: msg, Details: details}
}
func errNetwork(msg string) APIError {
	return APIError{Code: "URL_NOT_ACCESSIBLE", Type: "NetworkError", Message: msg}
}
func errNoFlow() APIError {
	return APIError{Code: "NO_FLOW_INFO", Type: "RequestInvalidError",
		Message: "No flow information available"}
}
