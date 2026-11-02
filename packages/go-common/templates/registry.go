// Package templates 提供实训模板的发现、校验与只读查询能力。
//
// 模板 = templates/<slug>/ 目录 + template.yaml 清单（v1 规范见仓库 templates/README.md）。
// 业务服务只依赖本包的 Registry 接口：默认文件实现扫描本地模板目录，
// 未来用户自定义模板可新增对象存储实现，content-service / sandbox-service 零改动。
package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/quenching/go-common/logger"
)

const (
	// APIVersionV1 模板清单 v1 的 apiVersion 取值。
	APIVersionV1 = "quenching/v1"
	// ManifestFile 每个模板目录下的清单文件名。
	ManifestFile = "template.yaml"

	// RoleBusiness 业务服务（默认），纳入恢复评分。
	RoleBusiness = "business"
	// RoleMiddleware 中间件/基础设施服务，不纳入恢复评分。
	RoleMiddleware = "middleware"
)

var (
	difficultyEnum = map[string]bool{"easy": true, "medium": true, "hard": true}
	// slug 同时兼作 Helm release 名：小写字母数字开头结尾，中间可含连字符，1-40 字符。
	slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
)

// Manifest 模板清单（template.yaml 反序列化结构）。
// 除 Slug/Name/Chart/Services 外全部可选；清单中的未知字段在解析时忽略
// （yaml.v3 默认不启用 KnownFields），保证旧程序读取新模板不会崩溃。
// json tag 与 yaml 同名，保证 content-service 对外输出 snake_case 契约；
// 前端只消费展示与部署所需字段，chart 等内部字段原样透出但不参与渲染。
type Manifest struct {
	ArtifactDigest       string            `yaml:"-" json:"artifact_digest"`
	BehaviorProtocolFile string            `yaml:"behavior_protocol_file,omitempty" json:"behavior_protocol_file,omitempty"`
	BehaviorProtocol     *BehaviorProtocol `yaml:"-" json:"behavior_protocol,omitempty"`
	APIVersion           string            `yaml:"apiVersion" json:"apiVersion"`
	Slug                 string            `yaml:"slug" json:"slug"`
	Name                 string            `yaml:"name" json:"name"`
	Domain               string            `yaml:"domain" json:"domain"`
	Description          string            `yaml:"description" json:"description"`
	BusinessScenarios    []string          `yaml:"-" json:"business_scenarios,omitempty"`
	LearningObjectives   []string          `yaml:"-" json:"learning_objectives,omitempty"`
	Overview             []string          `yaml:"-" json:"overview,omitempty"`
	Audience             []string          `yaml:"-" json:"audience,omitempty"`
	Cover                *CatalogCover     `yaml:"-" json:"cover,omitempty"`
	Difficulty           string            `yaml:"difficulty" json:"difficulty"` // easy | medium | hard
	TechStack            []string          `yaml:"tech_stack" json:"tech_stack"`
	Chart                string            `yaml:"chart" json:"chart"` // 模板目录内的 Chart 相对目录
	SortOrder            int               `yaml:"sort_order" json:"sort_order"`
	Enabled              *bool             `yaml:"enabled" json:"enabled"` // 缺省视为 true
	Services             []ServiceSpec     `yaml:"services" json:"services"`
	Images               []ImageSpec       `yaml:"images" json:"images"`
	Resources            *ResourcesSpec    `yaml:"resources" json:"resources"`
	// BehaviorFlow 真实用户行为流声明（可选）：沙箱就绪后由行为模拟引擎
	// 驱动项目自己的前端页面循环执行 steps，并采集浏览器侧失败信号。
	// 不声明该字段的模板（含裸沙箱）不启动行为模拟，不产生任何行为信号。
	BehaviorFlow *BehaviorFlowSpec `yaml:"behavior_flow,omitempty" json:"behavior_flow,omitempty"`
}

// BehaviorFlowSpec 行为流声明（模板清单的可选扩展，遵循"只加可选字段"治理）。
// 设计为声明式：步骤序列 + 循环间隔，运行时（Playwright）由 sandbox-service
// 统一提供，模板只描述"在页面上做什么"，不引入脚本代码。
type BehaviorFlowSpec struct {
	// Name 行为流名称，用于信号与排障展示；缺省取 "default"。
	Name string `yaml:"name" json:"name"`
	// EntryService 被测前端入口对应的 services[].name；
	// 缺省取第一个 role=business 的服务。
	EntryService string `yaml:"entry_service,omitempty" json:"entry_service,omitempty"`
	// EntryPath 入口路径，缺省 "/"；入口地址由 entry_service 集群内地址拼接。
	EntryPath string `yaml:"entry_path,omitempty" json:"entry_path,omitempty"`
	// IntervalSeconds 一轮行为流执行完后到下一轮的间隔秒数；<0 视为 0，缺省 5。
	IntervalSeconds int `yaml:"interval_seconds,omitempty" json:"interval_seconds,omitempty"`
	// TimeoutSeconds 单步默认超时秒数；缺省 15。
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// Steps 有序行为步骤，至少 1 步。
	Steps []BehaviorStep `yaml:"steps" json:"steps"`
}

// BehaviorStep 单个行为步骤。
//
// Action 取值：
//   - goto    ：跳转 URL（必填 url，支持相对路径，相对 ENTRY_URL 解析）
//   - click   ：点击元素（必填 selector）
//   - fill    ：填写输入框（必填 selector、value）
//   - wait    ：等待（selector 非空等待元素可见，否则等待 ms 毫秒）
//   - assert  ：断言（必填 selector；expect=visible|hidden|text，expect=text 时必填 value）
type BehaviorStep struct {
	Name     string `yaml:"name" json:"name"`     // 步骤名，进入信号，便于告警定位
	Action   string `yaml:"action" json:"action"` // goto | click | fill | wait | assert
	Selector string `yaml:"selector,omitempty" json:"selector,omitempty"`
	URL      string `yaml:"url,omitempty" json:"url,omitempty"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"`
	Expect   string `yaml:"expect,omitempty" json:"expect,omitempty"` // visible | hidden | text
	MS       int    `yaml:"ms,omitempty" json:"ms,omitempty"`
}

// ServiceSpec 模板内服务声明。
type ServiceSpec struct {
	DisplayName string              `yaml:"-" json:"display_name,omitempty"`
	Description string              `yaml:"-" json:"description,omitempty"`
	Elastic     *ElasticSpec        `yaml:"elastic,omitempty" json:"elastic,omitempty"`
	Resources   *ComponentResources `yaml:"resources,omitempty" json:"resources,omitempty"`
	Name        string              `yaml:"name" json:"name"`
	Workload    string              `yaml:"workload" json:"workload"` // K8s Deployment/StatefulSet 名，缺省取 Name
	Port        int                 `yaml:"port" json:"port"`
	Role        string              `yaml:"role" json:"role"` // business（默认）| middleware
}

type ElasticSpec struct {
	MaxReplicas int32 `yaml:"max_replicas" json:"max_replicas"`
}

// ImageSpec 需要随模板构建的私有镜像声明；全部走公共镜像的模板省略整段。
type ImageSpec struct {
	Name       string `yaml:"name" json:"name"` // 与 Services[].Name 对应
	Context    string `yaml:"context" json:"context"`
	Dockerfile string `yaml:"dockerfile" json:"dockerfile"`
}

// ResourcesSpec 单个沙箱实例的总资源预算（可选）。
type ResourcesSpec struct {
	Requests *ResourceAmounts `yaml:"requests,omitempty" json:"requests,omitempty"`
	Storage  string           `yaml:"storage,omitempty" json:"storage,omitempty"`
	CPU      string           `yaml:"cpu" json:"cpu"`
	Memory   string           `yaml:"memory" json:"memory"`
}

// IsEnabled 模板是否启用；enabled 缺省视为 true。
func (m Manifest) IsEnabled() bool { return m.Enabled == nil || *m.Enabled }

// Registry 模板只读注册表。业务服务只依赖本接口，不关心模板来自磁盘还是对象存储。
type Registry interface {
	// List 返回全部已启用模板，按 sort_order 升序、再按 slug 升序。
	List() []Manifest
	// Get 按 slug 取已启用模板；不存在或已停用时 ok=false。
	Get(slug string) (m Manifest, ok bool)
	// ChartPath 返回模板 Chart 在当前来源下可用于部署的本地路径。
	ChartPath(m Manifest) string
}

// Load 从模板根目录加载注册表：扫描 <root>/*/template.yaml。
// root 不存在/不可读时返回空注册表（服务照常启动）；
// 单个坏模板只记 warn 后跳过，不拖垮整个注册表。
func Load(root string) Registry {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		logger.L().Warn("模板根目录不存在或不可读，模板注册表为空", zap.String("root", root))
		return emptyRegistry{}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		logger.L().Warn("读取模板根目录失败，模板注册表为空", zap.String("root", root), zap.Error(err))
		return emptyRegistry{}
	}

	reg := &fileRegistry{root: root, items: make(map[string]Manifest)}
	seen := make(map[string]string) // slug -> 首个声明该 slug 的目录名（查重）

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
		if err != nil {
			continue // 不含 template.yaml 的目录不是模板，静默跳过
		}

		var m Manifest
		if err := yaml.Unmarshal(raw, &m); err != nil {
			logger.L().Warn("模板清单解析失败，跳过该模板", zap.String("dir", e.Name()), zap.Error(err))
			continue
		}
		if err := validate(&m, dir); err != nil {
			logger.L().Warn("模板清单校验失败，跳过该模板",
				zap.String("dir", e.Name()), zap.String("slug", m.Slug), zap.Error(err))
			continue
		}
		if first, dup := seen[m.Slug]; dup {
			logger.L().Warn("模板 slug 重复，跳过后者",
				zap.String("slug", m.Slug), zap.String("kept", first), zap.String("skipped", e.Name()))
			continue
		}
		seen[m.Slug] = e.Name()
		if err := loadCatalogPresentation(&m, dir); err != nil {
			logger.L().Warn("模板展示资料解析失败，保留清单中的展示信息", zap.String("slug", m.Slug), zap.Error(err))
		}
		reg.items[m.Slug] = m
	}

	reg.sorted = make([]Manifest, 0, len(reg.items))
	for _, m := range reg.items {
		if m.IsEnabled() {
			reg.sorted = append(reg.sorted, m)
		}
	}
	sort.Slice(reg.sorted, func(i, j int) bool {
		if reg.sorted[i].SortOrder != reg.sorted[j].SortOrder {
			return reg.sorted[i].SortOrder < reg.sorted[j].SortOrder
		}
		return reg.sorted[i].Slug < reg.sorted[j].Slug
	})
	return reg
}

// validate 校验清单并填充缺省值（workload 缺省=name，role 缺省=business）。
func validate(m *Manifest, dir string) error {
	if m.APIVersion != APIVersionV1 {
		return fmt.Errorf("不支持的 apiVersion %q（期望 %q）", m.APIVersion, APIVersionV1)
	}
	if !slugPattern.MatchString(m.Slug) {
		return fmt.Errorf("slug %q 非法：需小写字母数字开头结尾、中间可含连字符、长度 1-40", m.Slug)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(m.Chart) == "" {
		return fmt.Errorf("chart 不能为空")
	}
	// Chart 必须是模板目录内的相对路径，禁止绝对路径与目录逃逸。
	cleanChart := filepath.Clean(m.Chart)
	if filepath.IsAbs(cleanChart) || cleanChart == ".." || strings.HasPrefix(cleanChart, ".."+string(filepath.Separator)) {
		return fmt.Errorf("chart 路径非法（需为模板目录内相对路径）: %s", m.Chart)
	}
	chartInfo, err := os.Stat(filepath.Join(dir, cleanChart))
	if err != nil || !chartInfo.IsDir() {
		return fmt.Errorf("chart 目录不存在: %s", m.Chart)
	}
	if m.Difficulty != "" && !difficultyEnum[m.Difficulty] {
		return fmt.Errorf("difficulty %q 非法（可选值 easy | medium | hard）", m.Difficulty)
	}
	if len(m.Services) == 0 {
		return fmt.Errorf("services 至少声明 1 个服务")
	}

	names := make(map[string]bool, len(m.Services))
	for i := range m.Services {
		s := &m.Services[i]
		if s.Name == "" {
			return fmt.Errorf("services[%d].name 不能为空", i)
		}
		if names[s.Name] {
			return fmt.Errorf("services.name 重复: %s", s.Name)
		}
		names[s.Name] = true
		if s.Workload == "" {
			s.Workload = s.Name
		}
		if s.Role == "" {
			s.Role = RoleBusiness
		}
		if s.Role != RoleBusiness && s.Role != RoleMiddleware {
			return fmt.Errorf("services[%d].role %q 非法（business | middleware）", i, s.Role)
		}
	}

	for i, img := range m.Images {
		if img.Name == "" || img.Context == "" || img.Dockerfile == "" {
			return fmt.Errorf("images[%d] 的 name/context/dockerfile 均不能为空", i)
		}
		if !names[img.Name] {
			return fmt.Errorf("images[%d].name %q 未在 services 中声明", i, img.Name)
		}
	}

	if err := ValidateResourceContract(*m); err != nil {
		return err
	}
	if m.BehaviorProtocolFile != "" {
		protocol, err := loadBehaviorProtocol(dir, m.BehaviorProtocolFile)
		if err != nil {
			return err
		}
		if !names[protocol.EntryService] || !names[protocol.APIService] {
			return fmt.Errorf("protocol references undeclared service")
		}
		m.BehaviorProtocol = protocol
	}
	if mf := m.BehaviorFlow; mf != nil {
		if err := validateBehaviorFlow(mf, m.Services); err != nil {
			return err
		}
	}
	digest, err := TemplateArtifactDigest(dir)
	if err != nil {
		return err
	}
	m.ArtifactDigest = digest
	return nil
}

// 支持的行为步骤动作集合。
var behaviorActions = map[string]bool{
	"goto": true, "click": true, "fill": true, "wait": true, "assert": true,
}

// validateBehaviorFlow 校验行为流并填充缺省值。
func validateBehaviorFlow(f *BehaviorFlowSpec, services []ServiceSpec) error {
	if len(f.Steps) == 0 {
		return fmt.Errorf("behavior_flow.steps 至少需要 1 个步骤")
	}
	if f.Name == "" {
		f.Name = "default"
	}
	if f.IntervalSeconds < 0 {
		f.IntervalSeconds = 0
	}
	if f.IntervalSeconds == 0 {
		f.IntervalSeconds = 5
	}
	if f.TimeoutSeconds <= 0 {
		f.TimeoutSeconds = 15
	}
	if f.EntryPath == "" {
		f.EntryPath = "/"
	}

	// entry_service 缺省取第一个 business 服务；显式声明时必须存在。
	if f.EntryService == "" {
		for _, s := range services {
			if s.Role == RoleBusiness {
				f.EntryService = s.Name
				break
			}
		}
		if f.EntryService == "" {
			return fmt.Errorf("behavior_flow 无法确定入口服务：模板未声明 business 服务")
		}
	} else {
		found := false
		for _, s := range services {
			if s.Name == f.EntryService {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("behavior_flow.entry_service %q 未在 services 中声明", f.EntryService)
		}
	}

	stepNames := make(map[string]bool, len(f.Steps))
	for i, st := range f.Steps {
		if !behaviorActions[st.Action] {
			return fmt.Errorf("behavior_flow.steps[%d].action %q 非法（goto | click | fill | wait | assert）", i, st.Action)
		}
		if st.Name == "" {
			return fmt.Errorf("behavior_flow.steps[%d].name 不能为空", i)
		}
		if stepNames[st.Name] {
			return fmt.Errorf("behavior_flow.steps 名称重复: %s", st.Name)
		}
		stepNames[st.Name] = true

		switch st.Action {
		case "goto":
			if strings.TrimSpace(st.URL) == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）goto 动作必须提供 url", i, st.Name)
			}
		case "click":
			if st.Selector == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）click 动作必须提供 selector", i, st.Name)
			}
		case "fill":
			if st.Selector == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）fill 动作必须提供 selector", i, st.Name)
			}
		case "assert":
			if st.Selector == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）assert 动作必须提供 selector", i, st.Name)
			}
			if st.Expect == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）assert 动作必须提供 expect", i, st.Name)
			}
			if st.Expect != "visible" && st.Expect != "hidden" && st.Expect != "text" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）expect %q 非法（visible | hidden | text）", i, st.Name, st.Expect)
			}
			if st.Expect == "text" && st.Value == "" {
				return fmt.Errorf("behavior_flow.steps[%d]（%s）expect=text 时必须提供 value", i, st.Name)
			}
		}
	}
	return nil
}

// fileRegistry 基于本地模板目录的注册表实现。
type fileRegistry struct {
	root   string
	items  map[string]Manifest
	sorted []Manifest // 仅含已启用模板
}

func (r *fileRegistry) List() []Manifest {
	out := make([]Manifest, len(r.sorted))
	copy(out, r.sorted)
	return out
}

func (r *fileRegistry) Get(slug string) (Manifest, bool) {
	m, ok := r.items[slug]
	if !ok || !m.IsEnabled() {
		return Manifest{}, false
	}
	return m, true
}

func (r *fileRegistry) ChartPath(m Manifest) string {
	return filepath.Join(r.root, m.Slug, m.Chart)
}

// EmptyRegistry 返回空注册表（未配置模板源或测试场景使用）。
func EmptyRegistry() Registry { return emptyRegistry{} }

type emptyRegistry struct{}

func (emptyRegistry) List() []Manifest            { return nil }
func (emptyRegistry) Get(string) (Manifest, bool) { return Manifest{}, false }
func (emptyRegistry) ChartPath(Manifest) string   { return "" }
