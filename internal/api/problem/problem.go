package problem

import (
	"encoding/json"
	"net/http"
)

const ContentType = "application/problem+json"

// Details follows RFC 9457 and adds a stable machine-readable code.
type Details struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

func New(status int, code, detail string) Details {
	return Details{
		Type:   "https://filestore.local/problems/" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

func Write(writer http.ResponseWriter, details Details) {
	writer.Header().Set("Content-Type", ContentType)
	writer.WriteHeader(details.Status)
	_ = json.NewEncoder(writer).Encode(details)
}
