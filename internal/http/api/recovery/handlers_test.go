package recovery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeConfigurationBodyIsStrictAndBounded(t *testing.T) {
	type body struct {
		Value int `json:"value"`
	}
	for name, content := range map[string]string{
		"unknown field": `{"value":1,"future":true}`,
		"trailing JSON": `{"value":1} {}`,
		"oversized":     `{"value":1,"padding":"` + strings.Repeat("x", (1<<20)+1) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/recovery-policy", strings.NewReader(content))
			if err := decodeConfigurationBody(req, &body{}); err == nil {
				t.Fatal("invalid request body accepted")
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/recovery-policy", strings.NewReader(`{"value":1}`))
	var decoded body
	if err := decodeConfigurationBody(req, &decoded); err != nil || decoded.Value != 1 {
		t.Fatalf("valid request body decode = %#v, %v", decoded, err)
	}
}
