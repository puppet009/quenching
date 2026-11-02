package service

import "fmt"

// 章节解析相关错误。区分"AI 没数据/数据坏"与基础设施错误，
// worker 统一按失败处理（重试），但错误信息便于排查。
var (
	// ErrEmptySections AI 返回空内容。
	ErrEmptySections = fmt.Errorf("sections json is empty")
	// ErrIncompleteSections 根因/时间线任一章为空。
	ErrIncompleteSections = fmt.Errorf("sections incomplete: root_cause and timeline are required")
	// ErrIncompleteMethodSections 方法论复盘根因/时间线/路径对比任一章为空（T3-1）。
	ErrIncompleteMethodSections = fmt.Errorf("sections incomplete: root_cause, timeline and path_comparison are required")
)

// ParseError JSON 整体解析失败。
type ParseError struct {
	Cause error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse sections json: %v", e.Cause)
}

func (e *ParseError) Unwrap() error { return e.Cause }

// MissingFieldError 缺少必填章节字段。
type MissingFieldError struct {
	Field string
}

func (e *MissingFieldError) Error() string {
	return fmt.Sprintf("sections missing required field %q", e.Field)
}

// InputError 取数失败（PG 查询/下游数据端口异常）。
type InputError struct {
	Cause error
}

func (e *InputError) Error() string {
	return fmt.Sprintf("load review input: %v", e.Cause)
}

func (e *InputError) Unwrap() error { return e.Cause }

// AICallError 调用 ai-service 失败。
type AICallError struct {
	Cause error
}

func (e *AICallError) Error() string {
	return fmt.Sprintf("call ai-service GenerateReview: %v", e.Cause)
}

func (e *AICallError) Unwrap() error { return e.Cause }

// FieldTypeError 章节字段类型不是字符串。
type FieldTypeError struct {
	Field string
	Cause error
}

func (e *FieldTypeError) Error() string {
	return fmt.Sprintf("sections field %q is not a string: %v", e.Field, e.Cause)
}

func (e *FieldTypeError) Unwrap() error { return e.Cause }
