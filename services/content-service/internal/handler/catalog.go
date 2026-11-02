package handler

import (
	"crypto/subtle"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/content-service/internal/repository"
	"strconv"
)

func RegisterCatalog(r *gin.Engine, token string, d *repository.PGDefectRepo, p *repository.PitfallRepo) {
	guard := func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001})
			return
		}
		c.Next()
	}
	g := r.Group("/internal/v1/catalog", guard)
	g.GET("/defects", func(c *gin.Context) {
		q := repository.DefectQuery{TemplateSlug: c.Query("template_slug"), Keyword: c.Query("keyword"), Category: c.Query("category"), Difficulty: c.Query("difficulty"), DefectType: c.Query("defect_type")}
		q.ProjectID, _ = strconv.ParseInt(c.Query("project_id"), 10, 64)
		q.Page, _ = strconv.Atoi(c.Query("page"))
		q.PageSize, _ = strconv.Atoi(c.Query("page_size"))
		if q.Page < 1 {
			q.Page = 1
		}
		if q.PageSize < 1 || q.PageSize > 100 {
			q.PageSize = 100
		}
		if s := c.Query("is_active"); s != "" {
			v, e := strconv.ParseBool(s)
			if e != nil {
				c.JSON(400, gin.H{"code": 10001})
				return
			}
			q.IsActive = &v
		}
		data, err := d.List(c.Request.Context(), q)
		catalogReply(c, data, err)
	})
	g.GET("/defects/by-code", func(c *gin.Context) {
		id, e := strconv.ParseInt(c.Query("project_id"), 10, 64)
		if e != nil || id <= 0 || c.Query("code") == "" {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		data, err := d.GetByCode(c.Request.Context(), id, c.Query("code"))
		catalogReply(c, data, err)
	})
	g.GET("/defects/:id", func(c *gin.Context) {
		id, e := strconv.ParseInt(c.Param("id"), 10, 64)
		if e != nil || id <= 0 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		data, err := d.GetByID(c.Request.Context(), id)
		catalogReply(c, data, err)
	})
	g.POST("/pitfalls/by-defects", func(c *gin.Context) {
		var req struct {
			IDs []int64 `json:"ids"`
		}
		if c.ShouldBindJSON(&req) != nil || len(req.IDs) > 500 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		data, err := p.ListActiveByDefectIDs(c.Request.Context(), req.IDs)
		catalogReply(c, data, err)
	})
	g.GET("/pitfalls/:id", func(c *gin.Context) {
		id, e := strconv.ParseInt(c.Param("id"), 10, 64)
		if e != nil || id <= 0 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		data, err := p.GetByID(c.Request.Context(), id)
		catalogReply(c, data, err)
	})
}
func catalogReply(c *gin.Context, data any, err error) {
	if err != nil {
		status := 503
		if errors.Is(err, repository.ErrDefectNotFound) || errors.Is(err, repository.ErrPitfallConfigNotFound) {
			status = 404
		}
		c.JSON(status, gin.H{"code": 15003, "msg": "catalog query failed"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": data})
}

func catalogTokenGuard(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001})
			return
		}
		c.Next()
	}
}
