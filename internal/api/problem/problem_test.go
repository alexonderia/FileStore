package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	Write(recorder, New(http.StatusNotFound, "not_found", "resource was not found"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Content-Type"); got != ContentType {
		t.Fatalf("Content-Type = %q, want %q", got, ContentType)
	}
	var details Details
	if err := json.NewDecoder(recorder.Body).Decode(&details); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if details.Code != "not_found" || details.Status != http.StatusNotFound {
		t.Fatalf("details = %#v", details)
	}
}
