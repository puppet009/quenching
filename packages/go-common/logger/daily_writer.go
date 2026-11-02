package logger

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// dailyWriter serializes writes and closes yesterday's file before switching dates.
// Each Pod has its own filename so lumberjack is never shared by multiple processes.
type dailyWriter struct {
	mu       sync.Mutex
	config   FileConfig
	now      func() time.Time
	instance string
	day      string
	sink     *lumberjack.Logger
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	day := now.Format("2006-01-02")
	if w.day != day {
		if w.sink != nil {
			if err := w.sink.Close(); err != nil {
				return 0, err
			}
		}
		name := filepath.Base(w.config.Name)
		service := strings.TrimSuffix(name, filepath.Ext(name))
		if w.instance != "" {
			name = service + "-" + w.instance + filepath.Ext(name)
		}
		w.sink = &lumberjack.Logger{
			Filename: filepath.Join(w.config.Dir, service, day, name),
			MaxSize:  w.config.MaxSizeMB, MaxBackups: w.config.MaxBackups,
			MaxAge: w.config.MaxAgeDays, LocalTime: true, Compress: true,
		}
		w.day = day
		w.prune(service, now)
	}
	return w.sink.Write(p)
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.day = ""
	if w.sink == nil {
		return nil
	}
	err := w.sink.Close()
	w.sink = nil
	return err
}

// Expiration only removes this service's log files in dated directories. No
// recursive delete: unrelated files and nonempty directories always survive.
func (w *dailyWriter) prune(service string, now time.Time) {
	if w.config.MaxAgeDays <= 0 {
		return
	}
	root := filepath.Join(w.config.Dir, service)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -w.config.MaxAgeDays).Format("2006-01-02")
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if _, err := time.Parse("2006-01-02", entry.Name()); err != nil || entry.Name() >= cutoff {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			name := file.Name()
			own := name == service+".log" || strings.HasPrefix(name, service+"-")
			if own && !file.IsDir() && file.Type()&os.ModeSymlink == 0 && (strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".log.gz")) {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
		_ = os.Remove(dir)
	}
}

func fileInstance(name string) string {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return ""
		}
	}
	return name
}
