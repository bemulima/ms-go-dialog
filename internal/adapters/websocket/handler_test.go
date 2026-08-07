package websocket

import (
	"net/http/httptest"
	"testing"
)

func TestTicketProtocolRejectsQueryCredentials(t *testing.T) {
	request := httptest.NewRequest("GET", "http://example.test/api/v1/ws?ticket=leak", nil)
	response := httptest.NewRecorder()
	Handler{}.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("status=%d", response.Code)
	}
}
