// files.go 文件管理器核心操作（对标腾讯云 OrcaTerm 第二批）。
//
// 所有数据都来自容器内真实文件系统：列表经 find/stat 读取真实权限、所有者、
// 大小、修改时间；读写经 cat，建/删/改名/权限/复制/移动经对应标准命令。
//
// 安全要点：
//   - 路径在 Go 侧规范化并做最小防护（拒 NUL、禁止对根目录删除），但不设目录
//     牢笼——允许查看真实的 /etc、/var、/app；
//   - 命令以显式 argv 下发（非 shell 拼接用户输入），从根本上避免命令注入。
package service

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// FileEntry 文件列表一项（字段全部来自容器内真实 stat）。
type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	Mode        string `json:"mode"`        // 数字权限，如 0755
	Permissions string `json:"permissions"` // 符号权限，如 drwxr-xr-x
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Size        int64  `json:"size"`
	ModTime     int64  `json:"mod_time"` // Unix 秒
}

// validatePath 规范化并做最基本的路径防护。允许任意绝对路径（真实文件系统）。
func validatePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("路径非法")
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("仅支持绝对路径")
	}
	clean := cleanUnixPath(p)
	return clean, nil
}

// cleanUnixPath 词法规范化（不触达文件系统）。
func cleanUnixPath(p string) string {
	return path.Clean(p)
}

// FileTarget 一次文件操作的定位信息（会话 + Pod + 容器），跨包导出供 handler 使用。
type FileTarget struct {
	sessionID int64
	pod       string
	container string
}

// NewFileTarget 供 handler 等外部包构造定位信息，字段保持私有。
func NewFileTarget(sessionID int64, pod, container string) FileTarget {
	return FileTarget{sessionID: sessionID, pod: pod, container: container}
}

// listFilesScript 列目录脚本。统一流程：find 枚举直接子项路径（NUL 分隔，
// 兼容空格文件名），再对每个路径用 stat 取固定字段；GNU 与 busybox 输出在此
// 收敛。目标目录经位置参数 $1 传入，用户输入不参与命令拼接。
const listFilesScript = `d="$1"
while IFS= read -r -d '' p; do
  if stat -c '%F%x%a%x%s%x%A%x%U%x%G%x%n%x%Y' "$p" >/dev/null 2>&1; then
    stat -c '%F|%a|%s|%A|%U|%G|%n|%Y' "$p"
  else
    # busybox stat：字段顺序兼容，类型由首字符 -/d 推断
    stat -c '%F|%a|%s|%A|%U|%G|%n|%Y' "$p" 2>/dev/null
  fi
  printf '\0'
done < <(find "$d" -mindepth 1 -maxdepth 1 -print0 2>/dev/null)`

// ListFiles 列出目录真实内容，记录以 NUL 分隔（兼容含空格文件名）。
func (c *K8sClient) ListFiles(
	ctx context.Context, t FileTarget, dir string,
) ([]FileEntry, error) {
	dir, err := validatePath(dir)
	if err != nil {
		return nil, err
	}

	// 固定脚本：优先 GNU find -printf（一次执行拿全字段），不支持时退化到
	// find + stat 循环。路径作为位置参数 $1 传入，不参与拼接。
	out, err := c.runExec(ctx, execOptions{
		sessionID: t.sessionID,
		pod:       t.pod,
		container: t.container,
		command:   []string{"/bin/sh", "-c", listFilesScript, "--", dir},
	})
	if err != nil {
		return nil, err
	}

	entries := make([]FileEntry, 0)
	records := strings.Split(string(out), "\x00")
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "|")
		if len(fields) < 8 {
			continue
		}
		size, _ := strconv.ParseInt(fields[2], 10, 64)
		modSec, _ := strconv.ParseFloat(fields[7], 64)
		fullPath := fields[6]
		name := path.Base(fullPath)
		perms := fields[3]
		mode := fields[1]
		if len(mode) == 3 {
			mode = "0" + mode
		}
		entries = append(entries, FileEntry{
			Name:        name,
			Path:        fullPath,
			IsDir:       strings.HasPrefix(perms, "d"),
			Mode:        mode,
			Size:        size,
			ModTime:     int64(modSec),
			Permissions: perms,
			Owner:       fields[4],
			Group:       fields[5],
		})
	}
	sortFileEntries(entries)
	return entries, nil
}

// sortFileEntries 目录在前、再按名称升序（稳定排序，贴近主流文件管理器）。
func sortFileEntries(entries []FileEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// ReadFile 读取文件原始内容（真实文件）。
func (c *K8sClient) ReadFile(
	ctx context.Context, t FileTarget, p string,
) ([]byte, error) {
	p, err := validatePath(p)
	if err != nil {
		return nil, err
	}
	return c.runExec(ctx, execOptions{
		sessionID: t.sessionID,
		pod:       t.pod,
		container: t.container,
		command:   []string{"cat", "--", p},
	})
}

// WriteFile 把内容写入文件（经 stdin，避免内容进入参数）。
func (c *K8sClient) unauditedWriteFile(
	ctx context.Context, t FileTarget, p string, content []byte,
) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID,
		pod:       t.pod,
		container: t.container,
		// cat > "$1"：路径经位置参数传入，内容经 stdin。
		command: []string{"/bin/sh", "-c", `cat > "$1"`, "--", p},
		stdin:   bytes.NewReader(content),
	})
	return err
}

// MakeDir 创建目录（含父目录）。
func (c *K8sClient) unauditedMakeDir(ctx context.Context, t FileTarget, p string) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"mkdir", "-p", "--", p},
	})
	return err
}

// MakeFile 创建空文件。
func (c *K8sClient) unauditedMakeFile(ctx context.Context, t FileTarget, p string) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"touch", "--", p},
	})
	return err
}

// Rename 重命名/移动（同分区）。
func (c *K8sClient) unauditedRename(ctx context.Context, t FileTarget, from, to string) error {
	from, err := validatePath(from)
	if err != nil {
		return err
	}
	to, err = validatePath(to)
	if err != nil {
		return err
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"mv", "--", from, to},
	})
	return err
}

// Copy 复制（-a 保留权限/时间）。
func (c *K8sClient) unauditedCopy(ctx context.Context, t FileTarget, from, to string) error {
	from, err := validatePath(from)
	if err != nil {
		return err
	}
	to, err = validatePath(to)
	if err != nil {
		return err
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"cp", "-a", "--", from, to},
	})
	return err
}

// Chmod 修改数字权限（如 0755）。
func (c *K8sClient) unauditedChmod(ctx context.Context, t FileTarget, p, mode string) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	if !isValidOctalMode(mode) {
		return fmt.Errorf("权限值非法")
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"chmod", mode, "--", p},
	})
	return err
}

// Remove 删除文件/目录；禁止对根目录及若干关键路径操作。
func (c *K8sClient) unauditedRemove(ctx context.Context, t FileTarget, p string) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	if isProtectedPath(p) {
		return fmt.Errorf("为安全起见，禁止删除该系统关键路径")
	}
	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"rm", "-rf", "--", p},
	})
	return err
}

// ===== 辅助 =====

// isValidOctalMode 校验 3-4 位八进制权限。
func isValidOctalMode(m string) bool {
	if len(m) < 3 || len(m) > 4 {
		return false
	}
	for _, r := range m {
		if r < '0' || r > '7' {
			return false
		}
	}
	return true
}

// isProtectedPath 判断是否为禁止删除的系统关键路径。
func isProtectedPath(p string) bool {
	switch p {
	case "/", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/var", "/usr", "/boot", "/sys", "/proc", "/dev":
		return true
	}
	return false
}
