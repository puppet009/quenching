// Package response 提供统一的 HTTP 响应体 {code, msg, data}。
// 所有服务的 handler 必须使用本包返回，禁止各自写响应结构。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Body 统一响应结构。
type Body struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
	// TraceID 全链路追踪 ID，由中间件注入，便于排障。
	TraceID string `json:"trace_id,omitempty"`
}

// OK 返回成功响应。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{
		Code:    0,
		Msg:     "ok",
		Data:    data,
		TraceID: traceID(c),
	})
}

// Fail 返回业务错误响应（HTTP 200，错误码在 body 中）。
func Fail(c *gin.Context, e *Error) {
	c.JSON(http.StatusOK, Body{
		Code:    e.Code,
		Msg:     e.Msg,
		TraceID: traceID(c),
	})
}

// FailWithHTTP 返回需要带特定 HTTP 状态码的错误（如鉴权失败 401）。
func FailWithHTTP(c *gin.Context, httpStatus int, e *Error) {
	c.JSON(httpStatus, Body{
		Code:    e.Code,
		Msg:     e.Msg,
		TraceID: traceID(c),
	})
}

func traceID(c *gin.Context) string {
	if v, ok := c.Get("trace_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
