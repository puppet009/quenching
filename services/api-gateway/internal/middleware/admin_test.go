package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path, method, profile string
		want                  int
	}{
		{"/api/admin/v1/defects", "POST", `{"code":0,"data":{"permissions":["defect:manage"]}}`, 200},
		{"/api/admin/v1/sandboxes/7", "DELETE", `{"code":0,"data":{"permissions":["defect:manage"]}}`, 403},
		{"/api/v1/projects/2", "PUT", `{"code":0,"data":{"permissions":["training:join"]}}`, 403},
		{"/api/v1/admin/feedback/nps", "GET", `{"code":0,"data":{"permissions":["*"]}}`, 200},
		{"/api/admin/v1/defects", "GET", `{"code":1}`, 503},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-User-ID") != "9" {
					t.Errorf("trusted uid = %q", r.Header.Get("X-User-ID"))
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.profile))
			}))
			defer upstream.Close()
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set("uid", int64(9)) })
			r.Use(AdminAuth(upstream.URL))
			r.Any("/*path", func(c *gin.Context) { c.Status(200) })
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-User-ID", "1")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("got %d: %s want %d", w.Code, w.Body, tc.want)
			}
		})
	}
}
