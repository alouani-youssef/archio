package errors

import (
	"encoding/json"
	stderrors "errors"
	"log"
	"net/http"
)

// Error is an application error carrying an HTTP status and a stable code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// New returns an application error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json response: %v", err)
	}
}

// Write writes err as a JSON error response. Errors that are not *Error are
// logged and reported as an opaque 500.
func Write(w http.ResponseWriter, err error) {
	var appErr *Error
	if !stderrors.As(err, &appErr) {
		log.Printf("internal error: %v", err)
		appErr = New(http.StatusInternalServerError, "internal_error", "internal server error")
	}
	WriteJSON(w, appErr.Status, errorBody{Error: errorDetail{Code: appErr.Code, Message: appErr.Message}})
}
