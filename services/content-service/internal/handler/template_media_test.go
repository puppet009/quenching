package handler

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/templates"
)

func TestTemplateMediaOnlyServesRegisteredCover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	dir := filepath.Join(root, "mall")
	for _, sub := range []string{"helm", "catalog-assets"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "apiVersion: quenching/v1\nslug: mall\nname: 商城\nchart: helm\nservices:\n  - name: app\n"
	if err := os.WriteFile(filepath.Join(dir, "template.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: 商城实训\ndescription: 真实交易排查训练\ncover:\n  file: home.png\n  alt: 商城首页\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home.png", "unlisted.png"} {
		if err := os.WriteFile(filepath.Join(dir, "catalog-assets", name), imageData.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	router := func() *gin.Engine {
		r := gin.New()
		r.GET("/api/v1/template-media/:slug/:file", NewTemplateHandler(templates.Load(root)).Media)
		return r
	}
	r := router()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/template-media/mall/home.png", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), imageData.Bytes()) {
		t.Fatalf("public cover not served: %d", w.Code)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("ETag") == "" {
		t.Fatal("cover cache/content headers are missing")
	}
	cached := httptest.NewRequest(http.MethodGet, "/api/v1/template-media/mall/home.png", nil)
	cached.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, cached)
	if w.Code != http.StatusNotModified {
		t.Fatalf("conditional cover request: %d", w.Code)
	}
	for _, path := range []string{"mall/template.yaml", "mall/catalog.yaml", "mall/unlisted.png", "missing/home.png"} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/template-media/"+path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("unexpected asset exposure: %s (%d)", path, w.Code)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "template.yaml"), []byte(manifest+"enabled: false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/template-media/mall/home.png", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("disabled template still serves its cover")
	}
}
