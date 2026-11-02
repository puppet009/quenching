package logger

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStandardAndStructuredLogsReachServiceDateFiles(t *testing.T) {
	if dir := os.Getenv("QUENCHING_LOG_TEST_DIR"); dir != "" {
		InitWithFile("info", dir, "sandbox-service.log")
		log.Print("allocation session 5: deployment failed")
		L().Info("structured request")
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStandardAndStructuredLogsReachServiceDateFiles$")
	cmd.Env = append(os.Environ(), "QUENCHING_LOG_TEST_DIR="+dir, "POD_NAME=sandbox-test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "sandbox-service", "*", "*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("service/date log missing: %v %v", files, err)
	}
	if !strings.Contains(filepath.Base(files[0]), "sandbox-test") {
		t.Fatalf("Pod identity missing: %s", files[0])
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{"allocation session 5: deployment failed", "structured request"} {
		if !strings.Contains(string(raw), msg) || !strings.Contains(string(out), msg) {
			t.Fatalf("message absent from file or stdout: %s; file=%s stdout=%s", msg, raw, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("non-JSON log: %s", line)
		}
		if _, err := time.Parse(time.RFC3339Nano, entry["ts"].(string)); err != nil {
			t.Fatalf("timestamp without timezone: %v", err)
		}
	}
}

func TestDailyWriterChangesDateAndPrunesOnlyOwnExpiredLogs(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 10, 16, 23, 59, 59, 0, time.FixedZone("CST", 8*3600))
	w := &dailyWriter{config: FileConfig{Dir: dir, Name: "sandbox-service.log", MaxSizeMB: 1, MaxBackups: 2, MaxAgeDays: 30}, now: func() time.Time { return clock }}
	t.Cleanup(func() { _ = w.Close() })
	old := filepath.Join(dir, "sandbox-service", "2026-09-01")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "sandbox-service-old.log"), []byte("expired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "manual.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("first day\n")); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	if _, err := w.Write([]byte("next day\n")); err != nil {
		t.Fatal(err)
	}
	for day, want := range map[string]string{"2026-10-16": "first day", "2026-10-17": "next day"} {
		files, _ := filepath.Glob(filepath.Join(dir, "sandbox-service", day, "*.log"))
		if len(files) != 1 {
			t.Fatalf("date %s files: %v", day, files)
		}
		raw, err := os.ReadFile(files[0])
		if err != nil || !strings.Contains(string(raw), want) {
			t.Fatalf("date %s content: %s %v", day, raw, err)
		}
	}
	if _, err := os.Stat(filepath.Join(old, "sandbox-service-old.log")); !os.IsNotExist(err) {
		t.Fatalf("expired own file retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, "manual.txt")); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}
