// internal_readonly.go 服务间内部接口（Agent M3/M4）。
//
// 供 training-service 在编排助手工具时调用：在某会话沙箱内做只读观察或经审批
// 的写操作，不经 api-gateway、不对前端用户暴露，以 X-Internal-Token 校验东西向身份。
//
// 端点（统一前缀 /internal/v1/sandboxes/:session_id）：
//   - GET  /targets        列出沙箱业务服务器（Pod）
//   - GET  /files          列目录（query: path、pod?、container?）
//   - GET  /files/read     读文件（query: path、pod?、container?）
//   - POST /exec           执行只读诊断命令（白名单，body: pod/command/args/container?）
//   - POST /files/write    写文件（M4，调用前须经用户审批；body: pod/path/content/container?）
//
// exec 只允许白名单诊断命令（ps/ss/netstat 等），禁止任意 shell；写端点要求
// 显式目标 Pod、绝对路径，内容经 stdin 落盘（不参与命令拼接）。
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/sandbox-service/internal/service"
)

// InternalReadonlyHandler 内部只读接口处理器。
type InternalReadonlyHandler struct {
	kc    *service.K8sClient
	token string
}

// NewInternalReadonlyHandler 创建处理器。token 为空时不校验（本地开发）。
func NewInternalReadonlyHandler(kc *service.K8sClient, token string) *InternalReadonlyHandler {
	return &InternalReadonlyHandler{kc: kc, token: token}
}

// RegisterRoutes 注册内部只读路由。
func (h *InternalReadonlyHandler) RegisterRoutes(r gin.IRouter) {
	g := r.Group("/internal/v1/sandboxes", h.checkToken, func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithFileActor(c.Request.Context(), "assistant"))
		c.Next()
	})
	sid := func(c *gin.Context) (int64, bool) {
		id, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "session_id 非法"})
			return 0, false
		}
		return id, true
	}

	g.GET("/:session_id/targets", func(c *gin.Context) {
		id, ok := sid(c)
		if !ok {
			return
		}
		targets, err := h.kc.ListTargets(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50201, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": targets})
	})

	g.GET("/:session_id/files", func(c *gin.Context) {
		id, ok := sid(c)
		if !ok {
			return
		}
		dir := c.Query("path")
		if dir == "" {
			dir = "/"
		}
		t := service.NewFileTarget(id, c.Query("pod"), c.Query("container"))
		entries, err := h.kc.ListFiles(c.Request.Context(), t, dir)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50202, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok",
			"data": gin.H{"path": dir, "entries": entries}})
	})

	g.GET("/:session_id/files/read", func(c *gin.Context) {
		id, ok := sid(c)
		if !ok {
			return
		}
		p := c.Query("path")
		if p == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "path 必填"})
			return
		}
		t := service.NewFileTarget(id, c.Query("pod"), c.Query("container"))
		content, err := h.kc.ReadFile(c.Request.Context(), t, p)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50203, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok",
			"data": gin.H{"path": p, "content": string(content)}})
	})

	g.POST("/:session_id/exec", func(c *gin.Context) {
		id, ok := sid(c)
		if !ok {
			return
		}
		var req execReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "请求体非法"})
			return
		}
		command, err := buildReadonlyCommand(req)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"code": 40301, "msg": err.Error()})
			return
		}
		t := service.NewFileTarget(id, req.Pod, req.Container)
		out, err := h.kc.ExecCommand(c.Request.Context(), t, command)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50204, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok",
			"data": gin.H{"command": command, "output": string(out)}})
	})

	g.POST("/:session_id/files/write", func(c *gin.Context) {
		id, ok := sid(c)
		if !ok {
			return
		}
		var req internalWriteReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "请求体非法"})
			return
		}
		if strings.TrimSpace(req.Pod) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "pod 必填"})
			return
		}
		if strings.TrimSpace(req.Path) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "path 必填"})
			return
		}
		t := service.NewFileTarget(id, req.Pod, req.Container)
		if err := h.kc.WriteFile(c.Request.Context(), t, req.Path, []byte(req.Content)); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 50205, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok",
			"data": gin.H{"path": req.Path, "bytes": len(req.Content)}})
	})
}

// internalWriteReq 内部写文件请求体（M4，调用前须经用户审批）。
type internalWriteReq struct {
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// execReq 内部只读 exec 请求体。
type execReq struct {
	Pod       string   `json:"pod"`
	Container string   `json:"container"`
	Command   string   `json:"command"` // 白名单命令名，如 ps
	Args      []string `json:"args"`
}

// readonlyCommands 允许的只读诊断命令白名单。
var readonlyCommands = map[string]bool{
	"ps":       true,
	"ss":       true,
	"netstat":  true,
	"df":       true,
	"top":      true,
	"uptime":   true,
	"whoami":   true,
	"hostname": true,
	"free":     true,
}

// buildReadonlyCommand 校验命令是否在白名单、过滤危险参数。
func buildReadonlyCommand(req execReq) ([]string, error) {
	cmd := strings.TrimSpace(req.Command)
	if !readonlyCommands[cmd] {
		return nil, errForbidden(cmd)
	}
	// 参数白名单字符集：字母、数字、常见安全符号；拒绝 shell 元字符。
	for _, a := range req.Args {
		if !safeArg(a) {
			return nil, errForbiddenArg(a)
		}
	}
	command := append([]string{cmd}, req.Args...)
	return command, nil
}

func safeArg(a string) bool {
	if a == "" {
		return false
	}
	for _, r := range a {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == ' ' || r == ','
		if !allowed {
			return false
		}
	}
	return true
}

func (h *InternalReadonlyHandler) checkToken(c *gin.Context) {
	if h.token == "" {
		c.AbortWithStatusJSON(401, gin.H{"code": 40101, "msg": "internal authorization required"})
		return
	}
	if c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 40101, "msg": "内部令牌无效"})
	}
}

func errForbidden(cmd string) error {
	return errors.New("命令不在只读白名单内: " + cmd)
}

func errForbiddenArg(arg string) error {
	return errors.New("参数含非法字符: " + arg)
}
