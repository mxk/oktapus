package mock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// Server is a mock HTTP server.
type Server struct {
	Response map[string]interface{}
	Handler  http.Handler
}

// ClientServer returns an HTTP client connected to a mock server.
func ClientServer() (*http.Client, *Server) {
	s := &Server{Response: make(map[string]interface{})}
	s.Handler = s
	return &http.Client{Transport: s}, s
}

// RoundTrip implements http.RoundTripper.
func (s *Server) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	return w.Result(), nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if v := s.Response[r.URL.Path]; v != nil {
		switch v := v.(type) {
		case http.Handler:
			v.ServeHTTP(w, r)
		case http.HandlerFunc:
			v(w, r)
		case func(w http.ResponseWriter, r *http.Request):
			v(w, r)
		default:
			WriteJSON(w, v)
		}
		return
	}
	http.NotFound(w, r)
}

// WriteJSON writes JSON encoding of v to w.
func WriteJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}
