package httpapi

import (
	"encoding/json"
	"net/http"
)

// ErrorBody is the error object inside a failed envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Envelope is the standard response shape from PROJECT_CONTEXT §5.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data"`
	Error   *ErrorBody `json:"error"`
}

// WriteOK writes a successful envelope with HTTP 200 (or custom status).
func WriteOK(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, Envelope{Success: true, Data: data, Error: nil})
}

// WriteErr writes a failed envelope.
func WriteErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Envelope{
		Success: false,
		Data:    nil,
		Error:   &ErrorBody{Code: code, Message: message},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}
