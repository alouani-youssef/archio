package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWrite_AppError(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, New(http.StatusNotFound, "bucket_not_found", "bucket not found"))

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	want := `{"error":{"code":"bucket_not_found","message":"bucket not found"}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestWrite_UnknownError(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, fmt.Errorf("db password=x"))

	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaks internal detail: %s", rec.Body.String())
	}
	var body struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal_error" || body.Error.Message != "internal server error" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestWrite_WrappedAppError(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, fmt.Errorf("x: %w", New(http.StatusConflict, "bucket_exists", "bucket exists")))

	if rec.Code != 409 {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"bucket_exists"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
