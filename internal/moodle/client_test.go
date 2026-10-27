package moodle

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// Transport errors embed the request URL; the token must not leak through them.
func TestTransportErrorsOmitToken(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:9", Token: "SECRET123", HTTP: &http.Client{}}
	err := c.Call("core_webservice_get_site_info", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("Call error = %v", err)
	}
	_, err = c.Download("http://127.0.0.1:9/pluginfile.php/1/x.pdf", &bytes.Buffer{})
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("Download error = %v", err)
	}
}
