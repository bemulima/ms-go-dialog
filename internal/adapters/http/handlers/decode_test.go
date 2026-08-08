package handlers

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeOptionalEmptyBody_AcceptsOnlyEmptyRepresentations(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "empty", body: "", ok: true},
		{name: "whitespace", body: " \n\t ", ok: true},
		{name: "empty object", body: "{}", ok: true},
		{name: "empty object with whitespace", body: " \n{}\t", ok: true},
		{name: "null", body: "null"},
		{name: "array", body: "[]"},
		{name: "object with field", body: `{"unexpected":true}`},
		{name: "two objects", body: "{} {}"},
		{name: "trailing token", body: "{} true"},
		{name: "oversized chunked whitespace", body: strings.Repeat(" ", 1025)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("PUT", "http://example.test/command", io.NopCloser(strings.NewReader(test.body)))
			request.ContentLength = -1
			err := decodeOptionalEmptyBody(request)
			if test.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !test.ok && err == nil {
				t.Fatal("invalid empty-body representation was accepted")
			}
		})
	}
}
