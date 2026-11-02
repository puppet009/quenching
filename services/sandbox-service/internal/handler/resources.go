package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/auth"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"io"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ResourcesHandler struct {
	Elastic       *service.ElasticController
	InternalToken string
	Pool          service.ResourcePool
	Controls      service.ResourceControls
	Store         interface {
		ListActive(context.Context) ([]model.Sandbox, error)
	}
	JWTSecret, ProfileURL string
}

func (h ResourcesHandler) Register(r *gin.Engine) {
	g := r.Group("/api/admin/v1/resources", h.authorize)
	g.GET("", h.snapshot)
	g.PATCH("/nodes/:name/scheduling", h.scheduling)
	g.DELETE("/nodes/:name", h.remove)
	g.PATCH("/workloads/:namespace/:kind/:name", h.workload)
	g.PATCH("/elastic/:namespace/:name", h.elasticPolicy)
	h.registerScalingHolds(r)
}
func (h ResourcesHandler) authorize(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		c.AbortWithStatusJSON(401, gin.H{"code": 20001, "msg": "请先登录"})
		return
	}
	if h.JWTSecret == "" || h.ProfileURL == "" {
		c.AbortWithStatusJSON(503, gin.H{"code": 20005, "msg": "资源管理权限未配置"})
		return
	}
	claims, e := auth.NewManager(h.JWTSecret).ParseAccess(strings.TrimPrefix(header, "Bearer "))
	if e != nil || claims.UID <= 0 {
		c.AbortWithStatusJSON(401, gin.H{"code": 20002, "msg": "登录已失效"})
		return
	}
	req, e := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, h.ProfileURL, nil)
	if e != nil {
		c.AbortWithStatus(503)
		return
	}
	req.Header.Set("X-User-ID", strconv.FormatInt(claims.UID, 10))
	req.Header.Set("Authorization", header)
	client := http.Client{Timeout: 3 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		c.AbortWithStatusJSON(503, gin.H{"code": 20005, "msg": "权限服务不可用"})
		return
	}
	defer resp.Body.Close()
	var body struct {
		Code int `json:"code"`
		Data struct {
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.Code != 0 {
		c.AbortWithStatusJSON(403, gin.H{"code": 20003, "msg": "没有资源管理权限"})
		return
	}
	for _, p := range body.Data.Permissions {
		if p == "*" {
			c.Set("resource_admin_uid", claims.UID)
			c.Next()
			return
		}
	}
	c.AbortWithStatusJSON(403, gin.H{"code": 20003, "msg": "仅系统管理员可管理资源池"})
}
func resourceError(c *gin.Context, e error) {
	status := 400
	if errors.Is(e, service.ErrResourceConflict) || apierrors.IsConflict(e) {
		status = 409
	} else if apierrors.IsNotFound(e) {
		status = 404
	}
	c.JSON(status, gin.H{"code": 30010, "msg": e.Error()})
}
func (h ResourcesHandler) snapshot(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	read := h.Pool.Snapshot
	platformNamespace := h.Pool.PlatformNamespace
	if h.Elastic != nil {
		read = h.Elastic.Snapshot
		platformNamespace = h.Elastic.Pool.PlatformNamespace
	}
	s, e := read(ctx)
	if e != nil {
		c.JSON(503, gin.H{"code": 30011, "msg": "资源池暂不可用"})
		return
	}
	if h.Store != nil {
		s.Allocations, e = h.Store.ListActive(ctx)
		if e != nil {
			c.JSON(503, gin.H{"code": 30011, "msg": "资源分配记录暂不可用"})
			return
		}
	}
	if h.Elastic != nil {
		state, err := h.Elastic.Store.ElasticState(ctx)
		if err != nil {
			c.JSON(503, gin.H{"code": 30011, "msg": "弹性策略暂不可用"})
			return
		}
		for i := range state.Policies {
			reason := h.Elastic.FreezeReason(state.Policies[i], state.Holds)
			state.Policies[i].Frozen = reason != ""
			if reason != "" {
				state.Policies[i].Reason = reason
			}
		}
		s.Elastic = &state
	}
	missingTotal := model.ResourceAmounts{}
	for _, a := range s.Allocations {
		if a.ResourcesAdmitted {
			actual := s.NamespaceRequests[a.Namespace]
			missing := model.ResourceAmounts{CPUMilli: a.ResourcePlan.Requests.CPUMilli - actual.CPUMilli, MemoryBytes: a.ResourcePlan.Requests.MemoryBytes - actual.MemoryBytes}
			if missing.CPUMilli < 0 {
				missing.CPUMilli = 0
			}
			if missing.MemoryBytes < 0 {
				missing.MemoryBytes = 0
			}
			s.Reserved = s.Reserved.Add(missing)
			if s.NamespaceReserved == nil {
				s.NamespaceReserved = map[string]model.ResourceAmounts{}
			}
			s.NamespaceReserved[a.Namespace] = s.NamespaceReserved[a.Namespace].Add(missing)
			missingTotal = missingTotal.Add(missing)
		}
	}
	s.Available.CPUMilli -= missingTotal.CPUMilli
	s.Available.MemoryBytes -= missingTotal.MemoryBytes
	s.AllocationUnits = service.BuildAllocationUnits(s, platformNamespace)
	c.JSON(200, gin.H{"code": 0, "data": s})
}

type nodeChange struct {
	Version string `json:"resource_version" binding:"required"`
	UID     string `json:"uid" binding:"required"`
	Enabled *bool  `json:"enabled"`
}

func (h ResourcesHandler) scheduling(c *gin.Context) {
	var in nodeChange
	if c.ShouldBindJSON(&in) != nil || in.Enabled == nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "缺少节点版本或调度状态"})
		return
	}
	if e := h.Controls.SetNodeScheduling(c.Request.Context(), c.Param("name"), in.Version, in.UID, *in.Enabled); e != nil {
		resourceError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"updated": true}})
}
func (h ResourcesHandler) remove(c *gin.Context) {
	var in nodeChange
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "缺少节点版本"})
		return
	}
	if e := h.Controls.RemoveNode(c.Request.Context(), c.Param("name"), in.Version, in.UID); e != nil {
		resourceError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"removed": true}})
}
func (h ResourcesHandler) workload(c *gin.Context) {
	var in struct {
		Version   string                `json:"resource_version" binding:"required"`
		Container string                `json:"container" binding:"required"`
		Requests  model.ResourceAmounts `json:"requests"`
		Limits    model.ResourceAmounts `json:"limits"`
	}
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "资源设置无效"})
		return
	}
	update := func(ctx context.Context) error {
		return h.Controls.UpdatePlatformWorkload(ctx, c.Param("namespace"), c.Param("kind"), c.Param("name"), in.Container, in.Version, in.Requests, in.Limits)
	}
	var e error
	if h.Elastic != nil {
		e = h.Elastic.ManualWorkloadUpdate(c.Request.Context(), c.Param("namespace"), c.Param("name"), update)
	} else {
		e = update(c.Request.Context())
	}
	if e != nil {
		resourceError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"updated": true}})
}
