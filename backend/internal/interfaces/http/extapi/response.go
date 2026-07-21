// Package extapi 提供对外 API HTTP 响应格式（docs/api-external.md）。
package extapi

import (
	"encoding/json"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/vortexops/vortexops/pkg/apperr"
)

// Envelope 对外 API 统一响应。
type Envelope struct {
	Code      int    `json:"code"`
	Data      any    `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"requestId"`
}

// WriteOK 写入成功响应 code=0。
func WriteOK(w http.ResponseWriter, r *http.Request, data any) {
	writeEnvelope(w, http.StatusOK, Envelope{
		Code: 0, Data: data, RequestID: chimw.GetReqID(r.Context()),
	})
}

// WriteCreated 写入 201 成功响应。
func WriteCreated(w http.ResponseWriter, r *http.Request, data any) {
	writeEnvelope(w, http.StatusCreated, Envelope{
		Code: 0, Data: data, RequestID: chimw.GetReqID(r.Context()),
	})
}

// WriteError 写入错误响应（映射 docs/api-external.md 错误码）。
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := mapError(err)
	writeEnvelope(w, status, Envelope{
		Code: code, Message: msg, RequestID: chimw.GetReqID(r.Context()),
	})
}

func writeEnvelope(w http.ResponseWriter, status int, body Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func mapError(err error) (httpStatus, code int, msg string) {
	if err == nil {
		return http.StatusOK, 0, ""
	}
	e, ok := apperr.As(err)
	if !ok {
		return http.StatusInternalServerError, 50000, "internal server error"
	}
	msg = e.Message
	switch e.HTTPStatus {
	case http.StatusBadRequest:
		return e.HTTPStatus, 40000, msg
	case http.StatusUnauthorized:
		return e.HTTPStatus, 40100, msg
	case http.StatusForbidden:
		return e.HTTPStatus, 40300, msg
	case http.StatusNotFound:
		return e.HTTPStatus, 40400, msg
	case http.StatusConflict:
		return e.HTTPStatus, 40900, msg
	case http.StatusUnprocessableEntity:
		return e.HTTPStatus, 42200, msg
	case http.StatusTooManyRequests:
		return e.HTTPStatus, 42900, msg
	default:
		return e.HTTPStatus, 50000, msg
	}
}
