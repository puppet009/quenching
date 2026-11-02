package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func RegisterContentCompatibility(r *gin.Engine, base string) {
	upstream, err := url.Parse(base)
	valid := err == nil && upstream.Host != "" && (upstream.Scheme == "http" || upstream.Scheme == "https") && upstream.User == nil
	var proxy *httputil.ReverseProxy
	if valid {
		proxy = httputil.NewSingleHostReverseProxy(upstream)
		proxy.Transport = &http.Transport{ResponseHeaderTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
		proxy.ErrorHandler = func(w http.ResponseWriter, q *http.Request, e error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			w.Write([]byte(`{"code":15003,"msg":"content unavailable"}`))
		}
	}
	handle := func(c *gin.Context) {
		if !valid {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	}
	r.Any("/api/v1/admin/pitfalls", handle)
	r.Any("/api/v1/admin/pitfalls/*path", handle)
}
