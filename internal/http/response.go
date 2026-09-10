package http

import (
	"encoding/json"
	nethttp "net/http"

	"github.com/openark/orchestrator/internal/observability"
)

const defaultResponseCharset = "UTF-8"

type response struct {
	writer  nethttp.ResponseWriter
	request *nethttp.Request
}

var _ Responder = (*response)(nil)

func (response *response) JSON(status int, value any) {
	if api, ok := value.(*APIResponse); ok && api.Code == ERROR {
		observability.MarkBusinessError(response.request.Context())
	}
	contents, err := json.Marshal(value)
	if err != nil {
		nethttp.Error(response.writer, err.Error(), nethttp.StatusInternalServerError)
		return
	}
	response.writer.Header().Set("Content-Type", "application/json; charset="+defaultResponseCharset)
	response.writer.WriteHeader(status)
	_, _ = response.writer.Write(contents)
}

func (response *response) Redirect(location string, status ...int) {
	code := nethttp.StatusFound
	if len(status) == 1 {
		code = status[0]
	}
	nethttp.Redirect(response.writer, response.request, location, code)
}
