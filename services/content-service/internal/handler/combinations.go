package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func RegisterCombinations(r *gin.Engine, repo *repository.CombinationRepo, base, token string) {
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(ctx context.Context, method, path string, data any) (model.FaultCombination, error) {
		var out model.FaultCombination
		if base == "" || token == "" {
			return out, errors.New("fault validation unavailable")
		}
		raw, e := json.Marshal(data)
		if e != nil {
			return out, e
		}
		req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
		if e != nil {
			return out, e
		}
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(req)
		if e != nil {
			return out, e
		}
		defer resp.Body.Close()
		var body struct {
			Code int                    `json:"code"`
			Data model.FaultCombination `json:"data"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.Code != 0 {
			return out, errors.New("fault validation rejected")
		}
		return body.Data, nil
	}
	preview := func(c *gin.Context) {
		v, e := call(c.Request.Context(), "GET", "/internal/v1/faults/combinations/preview?"+c.Request.URL.RawQuery, nil)
		catalogReply(c, v, e)
	}
	g := r.Group("/api/admin/v1/defects/combinations")
	g.GET("/preview", preview)
	g.GET("", func(c *gin.Context) { v, e := repo.ListCombinations(c.Request.Context()); catalogReply(c, v, e) })
	g.POST("", func(c *gin.Context) {
		var v model.FaultCombination
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if c.ShouldBindJSON(&v) != nil || len(v.DefectIDs) != 2 || (v.Status != "draft" && v.Status != "verified") {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		if v.Status == "verified" {
			if _, e := call(c.Request.Context(), "POST", "/internal/v1/faults/combinations/validate", v); e != nil {
				c.JSON(400, gin.H{"code": 10001, "msg": e.Error()})
				return
			}
		}
		e := repo.SaveCombination(c.Request.Context(), v)
		catalogReply(c, v, e)
	})
	internal := r.Group("/internal/v1/catalog/combinations", catalogTokenGuard(token))
	internal.GET("", func(c *gin.Context) { v, e := repo.ListCombinations(c.Request.Context()); catalogReply(c, v, e) })
	internal.GET("/lookup", func(c *gin.Context) {
		ids := []int64{}
		for _, s := range strings.Split(c.Query("defect_ids"), ",") {
			id, e := strconv.ParseInt(s, 10, 64)
			if e != nil {
				c.JSON(400, gin.H{"code": 10001})
				return
			}
			ids = append(ids, id)
		}
		v, e := repo.Combination(c.Request.Context(), c.Query("template_slug"), ids)
		catalogReply(c, v, e)
	})
}
