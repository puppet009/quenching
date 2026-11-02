package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardMissingOwnersAndFeedbackPermission(t *testing.T) {
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "stats-token" {
			t.Error("missing internal token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"totalSessions":0,"avgScore":null,"npsScore":null}}`))
	}))
	defer owner.Close()
	for _, permissions := range [][]string{{"content:view"}, {"*"}} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("admin_permissions", permissions) })
		r.GET("/dashboard", DashboardHandler([]config.RouteRule{{PathPrefix: "/api/v1/training", TargetURL: owner.URL}}, "stats-token"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/dashboard", nil))
		var out struct {
			Data struct {
				Stats       map[string]interface{} `json:"stats"`
				Unavailable []string               `json:"unavailable"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Data.Unavailable) != 4 {
			t.Fatalf("missing owners must be unavailable: %s", w.Body.String())
		}
		if out.Data.Stats["totalSessions"] != float64(0) {
			t.Fatal("real zero lost")
		}
		if v, exists := out.Data.Stats["avgScore"]; !exists || v != nil {
			t.Fatal("null average lost")
		}
		_, hasNPS := out.Data.Stats["npsScore"]
		if hasNPS != (permissions[0] == "*") {
			t.Fatal("feedback permission ignored")
		}
	}
}
