// Package apperr 定义 VortexOps 统一应用错误模型。
// 错误在领域层构造，由 interfaces/http 层转换为 HTTP 响应。
// 设计目标：错误类型可被程序判断（NotFound/Conflict/Validation 等），
// 同时携带面向用户的可读消息与 trace ID。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code 是稳定的错误码枚举，对外暴露在 API 响应中。
type Code string

const (
	CodeInternal      Code = "internal_error"
	CodeUnauthorized  Code = "unauthorized"
	CodeForbidden     Code = "forbidden"
	CodeNotFound      Code = "not_found"
	CodeConflict      Code = "conflict"
	CodeValidation    Code = "validation_error"
	CodeRateLimited   Code = "rate_limited"
	CodeUnavailable   Code = "service_unavailable"
	CodePrecondition  Code = "precondition_failed"
	CodeBusinessRule  Code = "business_rule_violation"
)

// Error 是 VortexOps 统一错误类型，实现 error 接口。
type Error struct {
	Code       Code
	Message    string // 面向用户的可读消息（不含敏感信息）
	HTTPStatus int
	Cause      error  // 原始错误（仅日志记录，不返回客户端）
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is/As。
func (e *Error) Unwrap() error { return e.Cause }

// New 构造一个错误。
func New(code Code, msg string, httpStatus int, cause error) *Error {
	return &Error{Code: code, Message: msg, HTTPStatus: httpStatus, Cause: cause}
}

// Newf 构造带格式化消息的错误。
func Newf(code Code, httpStatus int, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), HTTPStatus: httpStatus, Cause: cause}
}

// 预设构造器。

func Unauthorized(msg string, cause error) *Error {
	return New(CodeUnauthorized, msg, http.StatusUnauthorized, cause)
}

func Forbidden(msg string, cause error) *Error {
	return New(CodeForbidden, msg, http.StatusForbidden, cause)
}

func NotFound(resource, id string) *Error {
	return Newf(CodeNotFound, http.StatusNotFound, nil, "%s not found: %s", resource, id)
}

func Conflict(msg string, cause error) *Error {
	return New(CodeConflict, msg, http.StatusConflict, cause)
}

func Validation(msg string, cause error) *Error {
	return New(CodeValidation, msg, http.StatusBadRequest, cause)
}

func BusinessRule(msg string, cause error) *Error {
	return New(CodeBusinessRule, msg, http.StatusUnprocessableEntity, cause)
}

func RateLimited(msg string, cause error) *Error {
	return New(CodeRateLimited, msg, http.StatusTooManyRequests, cause)
}

func Internal(msg string, cause error) *Error {
	return New(CodeInternal, msg, http.StatusInternalServerError, cause)
}

// As 试图把 err 解析为 *Error，成功返回该错误，否则 nil。
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// HTTPStatusOf 返回错误对应的 HTTP 状态码，未知错误返回 500。
func HTTPStatusOf(err error) int {
	if e, ok := As(err); ok {
		return e.HTTPStatus
	}
	return http.StatusInternalServerError
}

// CodeOf 返回错误码，未知错误返回 CodeInternal。
func CodeOf(err error) Code {
	if e, ok := As(err); ok {
		return e.Code
	}
	return CodeInternal
}
