package response

import (
	"fmt"
	"sync"
)

// Error 业务错误。Code 为全局错误码，跨服务统一在 errno 表登记。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Msg)
}

// New 构造一个业务错误。
func New(code int, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

// 预定义通用错误码（服务自定义错误码请使用各服务的段位，避免冲突）。
var (
	ErrSuccess      = &Error{Code: 0, Msg: "ok"}
	ErrInternal     = &Error{Code: 10000, Msg: "服务器内部错误"}
	ErrParam        = &Error{Code: 10001, Msg: "参数错误"}
	ErrUnauthorized = &Error{Code: 10002, Msg: "未登录或登录已过期"}
	ErrForbidden    = &Error{Code: 10003, Msg: "无权限"}
	ErrNotFound     = &Error{Code: 10004, Msg: "资源不存在"}
	ErrConflict     = &Error{Code: 10005, Msg: "资源冲突"}
)

// ErrBadRequest 请求参数错误，与 ErrParam 同码（HTTP 语义别名，handler 按名取用）。
var ErrBadRequest = ErrParam

// sentinelRegistry 按错误码缓存哨兵 error。
// Unwrap() 需要返回稳定可比较的实例：仓储层返回 ErrXxx.Unwrap()，
// handler 层用 errors.Is(err, ErrXxx.Unwrap()) 判型，两次调用必须命中同一实例。
var (
	sentinelMu       sync.Mutex
	sentinelRegistry = map[int]error{}
)

// Unwrap 返回按 Code 缓存的哨兵 error，使业务错误支持 errors.Is / errors.As 比较。
// 同码的 *Error（含动态文案错误，如配额超限）共享同一哨兵。
func (e *Error) Unwrap() error {
	sentinelMu.Lock()
	defer sentinelMu.Unlock()
	if s, ok := sentinelRegistry[e.Code]; ok {
		return s
	}
	s := &sentinelError{code: e.Code}
	sentinelRegistry[e.Code] = s
	return s
}

// sentinelError 与具体文案解耦的码级哨兵 error。
type sentinelError struct{ code int }

func (s *sentinelError) Error() string { return fmt.Sprintf("[%d]", s.code) }
