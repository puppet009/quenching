// 助手修改记录模型（M5「修改操作登记+复盘」）。
//
// training-service 逐次登记 write_file 修改，review-service 经服务间接口
// 取回并规则生成"助手修改记录"确定性章节。本文件只承载取回的数据结构，
// 不参与 AI prompt（修改记录以确定性章节呈现，避免 LLM 臆造）。
package model

// ChangeItem 一次文件修改记录。JSON 标签与 training service.ReviewChangeItem 对齐。
type ChangeItem struct {
	ID            int64  `json:"id"`
	Pod           string `json:"pod"`
	Container     string `json:"container,omitempty"`
	Path          string `json:"path"`
	BeforeExisted bool   `json:"before_existed"`
	BeforeContent string `json:"before_content,omitempty"`
	AfterContent  string `json:"after_content"`
	OffsetSec     int    `json:"t"`
}

// ChangeAnalysis 一次会话的全部修改记录（章节结构化载荷）。
type ChangeAnalysis struct {
	TotalCount int          `json:"total_count"`
	Changes    []ChangeItem `json:"changes"`
}

// 助手修改记录章节的固定取值（M5）。
//
// §10.2 初版词表未单列该类型，与 T3 系列专属章节同例先取独立值
// （assistant_changes），词表回填时对齐；sort_order=70 排在方法论
// 六章（10~60）之后。章节级别由 worker 按当次报告级别统一对齐，
// 此处默认 basic 仅为落库非空约束。
const (
	SectionTypeAssistantChanges = "assistant_changes"
	ChangeSectionTitle          = "助手修改记录"
	ChangeSectionOrder          = 70
	ChangeSectionLevel          = SectionLevelBasic
)
