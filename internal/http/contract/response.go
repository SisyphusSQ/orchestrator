package contract

import (
	"encoding/json"
	"net/http"
)

// ResponseCode is the stable business response code used by the HTTP API.
type ResponseCode int

const (
	ERROR ResponseCode = iota
	OK
)

func (code *ResponseCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(code.String())
}

func (code *ResponseCode) String() string {
	switch *code {
	case ERROR:
		return "ERROR"
	case OK:
		return "OK"
	}
	return "unknown"
}

// HTTPStatus returns the HTTP status corresponding to the business response.
func (code *ResponseCode) HTTPStatus() int {
	switch *code {
	case ERROR:
		return http.StatusInternalServerError
	case OK:
		return http.StatusOK
	}
	return http.StatusNotImplemented
}

// Response is the stable JSON response envelope used by API endpoints.
type Response struct {
	Code       ResponseCode
	Message    string
	Details    any
	ErrorClass string `json:",omitempty"`
}

// IsBusinessError lets the transport classify failed API operations without
// depending on endpoint packages.
func (response Response) IsBusinessError() bool {
	return response.Code == ERROR
}
