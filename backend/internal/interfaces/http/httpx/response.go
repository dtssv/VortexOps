// Package httpx 提供 HTTP 层共享工具：统一 JSON 响应、分页、请求绑定与校验。
package httpx

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/vortexops/vortexops/pkg/apperr"
)

// Response 是统一 API 响应信封。
type Response struct {
	Success bool `json:"success"`
	Data    any  `json:"data,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

// ErrorBody 错误响应体。
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Paged 分页数据包装。
type Paged[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

// OK 写入 200 成功响应。
func OK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, Response{Success: true, Data: data})
}

// Created 写入 201 成功响应。
func Created(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, Response{Success: true, Data: data})
}

// WriteError 把应用错误转为 HTTP 响应。
func WriteError(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, Response{Success: true})
		return
	}
	e, ok := apperr.As(err)
	if !ok {
		log.Printf("[ERROR] unclassified internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, Response{
			Success: false,
			Error:   &ErrorBody{Code: string(apperr.CodeInternal), Message: "internal server error"},
		})
		return
	}
	// 服务端错误记录原始 cause，便于排查（消息本身不含敏感信息，仍返回客户端）。
	if e.HTTPStatus >= http.StatusInternalServerError {
		log.Printf("[ERROR] %s %s: %v", e.Code, e.Message, e.Cause)
	}
	writeJSON(w, e.HTTPStatus, Response{
		Success: false,
		Error:   &ErrorBody{Code: string(e.Code), Message: e.Message},
	})
}

// NoContent 写入 204。
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Accepted 写入 202（异步操作已接受）。
func Accepted(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusAccepted, Response{Success: true, Data: data})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// QueryInt 从 query 解析 int，缺省返回 def。
func QueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Pagination 从请求解析分页参数（page 从 1 起，size 默认 20，上限 100）。
func Pagination(r *http.Request) (page, size, offset int) {
	page = QueryInt(r, "page", 1)
	size = QueryInt(r, "size", 20)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	offset = (page - 1) * size
	return
}
