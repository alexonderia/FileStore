package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/alexonderia/filestore/internal/api/problem"
)

type healthResponse struct {
	Status string `json:"status"`
}

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health("live"))
	mux.HandleFunc("GET /health/ready", health("ready"))
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health/live" || request.URL.Path == "/health/ready" {
			writer.Header().Set("Allow", http.MethodGet)
			details := problem.New(http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed for this resource")
			details.Instance = request.URL.Path
			problem.Write(writer, details)
			return
		}
		details := problem.New(http.StatusNotFound, "not_found", "resource was not found")
		details.Instance = request.URL.Path
		problem.Write(writer, details)
	})
	return mux
}

func health(status string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(writer).Encode(healthResponse{Status: status})
	}
}
