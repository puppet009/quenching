// files.go 文件管理器 HTTP 端点（对标腾讯云 OrcaTerm 第二批）。
//
// 挂载在 /api/v1/sandboxes/:id/files/*（组经 UserID() 鉴权）：
//
//	GET  list/read/download                 path=<绝对路径>
//	POST write/mkdir/touch/rename/chmod/copy/move/remove   JSON 请求体
//	POST upload  path=<目标目录>&on_exist=skip|replace|keep_both  multipart
//
// 所有路径必须为绝对路径，最终操作在对应 Pod/容器的真实文件系统内执行。
package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"

	"github.com/quenching/sandbox-service/internal/service"
)

// FilesHandler 文件管理器处理器。
type FilesHandler struct {
	kc    *service.K8sClient
	audit trainingAuditSink
	Owned func(context.Context, int64, int64) error
}

// NewFilesHandler 创建文件管理器处理器。
func NewFilesHandler(kc *service.K8sClient) *FilesHandler {
	return &FilesHandler{kc: kc}
}

// maxUploadMemory multipart 内存上限，超出转磁盘临时文件。
const maxUploadMemory = 20 << 20 // 20MB

// maxUploadSize 单次上传总大小上限。
const maxUploadSize = 500 << 20 // 500MB（对标腾讯云免费版）

// RegisterFiles 注册文件管理器路由（g 为 /api/v1/sandboxes 组）。
func (h *FilesHandler) RegisterFiles(g *gin.RouterGroup) {
	fg := g.Group("/:id/files")
	fg.Use(func(c *gin.Context) {
		if h.Owned != nil {
			uid, _ := c.Get(ContextUID)
			u, _ := uid.(int64)
			sid, _ := strconv.ParseInt(c.Param("id"), 10, 64)
			if h.Owned(c.Request.Context(), u, sid) != nil {
				c.AbortWithStatusJSON(404, gin.H{"code": 14004})
				return
			}
		}
		if c.Request.Method == "POST" {
			h.auditMutation(c)
		} else {
			c.Next()
		}
	})
	fg.GET("/list", h.list)
	fg.GET("/read", h.read)
	fg.GET("/download", h.download)
	fg.POST("/write", h.write)
	fg.POST("/mkdir", h.mkdir)
	fg.POST("/touch", h.touch)
	fg.POST("/rename", h.rename)
	fg.POST("/chmod", h.chmod)
	fg.POST("/copy", h.copy)
	fg.POST("/move", h.move)
	fg.POST("/remove", h.remove)
	fg.POST("/upload", h.upload)
}

// parseTarget 解析会话 ID 与定位的 Pod/容器。
func parseTarget(c *gin.Context) (service.FileTarget, error) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		return service.FileTarget{}, fmt.Errorf("会话ID非法")
	}
	pod := c.Query("pod")
	if pod == "" {
		return service.FileTarget{}, fmt.Errorf("缺少目标服务器")
	}
	return service.NewFileTarget(sessionID, pod, c.Query("container")), nil
}

// list 列目录。
func (h *FilesHandler) list(c *gin.Context) {
	t, err := parseTarget(c)
	if err != nil {
		failFiles(c, err)
		return
	}
	entries, err := h.kc.ListFiles(c.Request.Context(), t, c.Query("path"))
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, entries)
}

// read 读文件内容。
func (h *FilesHandler) read(c *gin.Context) {
	t, err := parseTarget(c)
	if err != nil {
		failFiles(c, err)
		return
	}
	content, err := h.kc.ReadFile(c.Request.Context(), t, c.Query("path"))
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, map[string]string{"content": string(content)})
}

// writeReq 写文件请求。
type writeReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// write 写文件（编辑器保存）。
func (h *FilesHandler) write(c *gin.Context) {
	t, err := parseTarget(c)
	if err != nil {
		failFiles(c, err)
		return
	}
	var req writeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failFiles(c, err)
		return
	}
	if err := h.kc.WriteFile(c.Request.Context(), t, req.Path, []byte(req.Content)); err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"saved": true})
}

// simplePathReq 只含一个路径的请求（mkdir/touch/remove）。
type simplePathReq struct {
	Path string `json:"path"`
}

func (h *FilesHandler) mkdir(c *gin.Context) {
	t, err := parseTarget(c)
	if err == nil {
		var req simplePathReq
		err = c.ShouldBindJSON(&req)
		if err == nil {
			err = h.kc.MakeDir(c.Request.Context(), t, req.Path)
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *FilesHandler) touch(c *gin.Context) {
	t, err := parseTarget(c)
	if err == nil {
		var req simplePathReq
		err = c.ShouldBindJSON(&req)
		if err == nil {
			err = h.kc.MakeFile(c.Request.Context(), t, req.Path)
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// twoPathReq 含源/目标路径的请求（rename/copy/move）。
type twoPathReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (h *FilesHandler) rename(c *gin.Context) {
	h.runTwoPath(c, h.kc.Rename)
}

func (h *FilesHandler) copy(c *gin.Context) {
	h.runTwoPath(c, h.kc.Copy)
}

// twoPathFunc 两路径操作签名（rename/copy/move）。
type twoPathFunc func(ctx context.Context, t service.FileTarget, from, to string) error

func (h *FilesHandler) runTwoPath(c *gin.Context, fn twoPathFunc) {
	t, err := parseTarget(c)
	if err == nil {
		var req twoPathReq
		err = c.ShouldBindJSON(&req)
		if err == nil {
			err = fn(c.Request.Context(), t, req.From, req.To)
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// chmodReq 权限修改请求。
type chmodReq struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

func (h *FilesHandler) chmod(c *gin.Context) {
	t, err := parseTarget(c)
	if err == nil {
		var req chmodReq
		err = c.ShouldBindJSON(&req)
		if err == nil {
			err = h.kc.Chmod(c.Request.Context(), t, req.Path, req.Mode)
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *FilesHandler) move(c *gin.Context) {
	// 移动跨分区时 mv 可能失败，这里直接用 Rename（容器内同根文件系统一般可用）。
	h.runTwoPath(c, h.kc.Rename)
}

func (h *FilesHandler) remove(c *gin.Context) {
	t, err := parseTarget(c)
	if err == nil {
		var req simplePathReq
		err = c.ShouldBindJSON(&req)
		if err == nil {
			err = h.kc.Remove(c.Request.Context(), t, req.Path)
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// upload multipart 上传（支持文件与文件夹，条目的相对路径取表单文件名）。
func (h *FilesHandler) upload(c *gin.Context) {
	t, err := parseTarget(c)
	if err != nil {
		failFiles(c, err)
		return
	}
	destDir := c.Query("path")
	onExist := c.DefaultQuery("on_exist", "replace")

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
	if err := c.Request.ParseMultipartForm(maxUploadMemory); err != nil {
		failFiles(c, fmt.Errorf("解析上传内容失败（可能超出 %d 大小限制）", maxUploadSize))
		return
	}
	form := c.Request.MultipartForm
	files := form.File["files"]
	if len(files) == 0 {
		failFiles(c, fmt.Errorf("没有待上传文件"))
		return
	}

	uploads := make([]service.UploadFile, 0, len(files))
	for _, fh := range files {
		opened, oErr := fh.Open()
		if oErr != nil {
			failFiles(c, oErr)
			return
		}
		rel := strings.ReplaceAll(fh.Filename, "\\", "/")
		uploads = append(uploads, service.UploadFile{
			RelativePath: rel,
			Content:      opened,
			Size:         fh.Size,
		})
	}

	result, err := h.kc.UploadInto(c.Request.Context(), t, destDir, uploads, onExist)
	// 关闭此前打开的文件句柄（reader 已被读取完毕）。
	for i := range uploads {
		if closer, ok := uploads[i].Content.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	if err != nil {
		failFiles(c, err)
		return
	}
	response.OK(c, result)
}

// download 流式下载（文件原样、目录转 zip）。响应头必须在写流之前设置。
func (h *FilesHandler) download(c *gin.Context) {
	t, err := parseTarget(c)
	if err != nil {
		failFiles(c, err)
		return
	}

	// 先确定下载文件名，设置好处置头与内容类型，再开始写流。
	// 文件名需要依据是否目录而定：容器内判定一次。
	p := c.Query("path")
	name, err := h.previewDownloadName(c.Request.Context(), t, p)
	if err != nil {
		failFiles(c, err)
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(name)))
	c.Status(http.StatusOK)

	if err := h.kc.StreamDownload(c.Request.Context(), t, p, c.Writer); err != nil {
		// 已开始写响应体，无法再切换为 JSON 错误；连接中断由浏览器侧体现。
		return
	}
}

// previewDownloadName 只确定下载文件名（单文件=原名，目录=原名.zip）。
func (h *FilesHandler) previewDownloadName(
	ctx context.Context, t service.FileTarget, p string,
) (string, error) {
	return h.kc.PreviewDownloadName(ctx, t, p)
}

// failFiles 业务错误统一返回（参数错误 10001，其余 15001）。
func failFiles(c *gin.Context, err error) {
	if errors.Is(err, service.ErrFileAuditUnconfirmed) {
		c.JSON(503, gin.H{"code": 15003, "msg": err.Error(), "outcome_unconfirmed": true})
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "非法") || strings.Contains(msg, "为空") || strings.Contains(msg, "缺少") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": msg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": msg})
}

// failFilesIfNotWritten 已开始写流时无法回退，仅记录；否则按业务错误返回。
func failFilesIfNotWritten(c *gin.Context, err error) {
	if c.Writer.Written() {
		return
	}
	failFiles(c, err)
}
