package templates

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CatalogCover describes one curated display image, never a runtime artifact.
// Filesystem paths and content metadata are omitted from the public API.
type CatalogCover struct {
	File        string `yaml:"file" json:"-"`
	Alt         string `yaml:"alt" json:"alt"`
	URL         string `yaml:"-" json:"url"`
	Width       int    `yaml:"-" json:"width"`
	Height      int    `yaml:"-" json:"height"`
	AssetPath   string `yaml:"-" json:"-"`
	ContentType string `yaml:"-" json:"-"`
	ETag        string `yaml:"-" json:"-"`
}

var coverFilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.(png|jpg|jpeg)$`)

func loadCatalogCover(cover *CatalogCover, dir, slug string) error {
	if cover == nil {
		return nil
	}
	if !coverFilePattern.MatchString(cover.File) || strings.TrimSpace(cover.Alt) == "" {
		return fmt.Errorf("catalog cover requires a PNG/JPEG basename and alt text")
	}
	mediaDir := filepath.Join(dir, "catalog-assets")
	info, err := os.Lstat(mediaDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("catalog-assets must be a real directory")
	}
	path := filepath.Join(mediaDir, cover.File)
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return fmt.Errorf("catalog cover must be a regular image of at most 2 MiB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return fmt.Errorf("catalog cover must be a valid PNG/JPEG, at most 4096 pixels per side")
	}
	if format != "png" && format != "jpeg" {
		return fmt.Errorf("unsupported catalog image format")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	cover.AssetPath, cover.ContentType = path, "image/"+format
	cover.Width, cover.Height = config.Width, config.Height
	cover.ETag = `"` + digest + `"`
	cover.URL = "/api/v1/template-media/" + slug + "/" + cover.File + "?v=" + digest[:12]
	return nil
}
