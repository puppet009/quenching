// files_transfer.go 文件/文件夹的上传下载（文件管理器第二批）。
//
// 上传：浏览器 multipart（字段带相对路径，支持文件夹）→ 服务端按相对路径打成
// tar 流 → 容器内 tar -x 解包（保留可执行位与目录结构）；同名由 onExist 策略
// 决定（skip/replace/keep_both）。
//
// 下载：容器内对目标打 tar → 服务端流式转 zip 写到浏览器（单文件直接给原文件）。
// 全程流式，不在服务端落临时大文件。
package service

import (
	"archive/tar"
	"archive/zip"
	"context"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"io"
	"path"
	"strings"
)

// UploadFile 浏览器上传的一个条目：相对路径（含目录）+ 内容。
type UploadFile struct {
	// RelativePath 相对目标目录的路径，如 "conf/a.yaml" / "a.txt"。
	RelativePath string
	Content      io.Reader
	Size         int64
	Mode         int64 // Unix 权限，默认 0644；可执行给 0755
}

// UploadResult 上传结果。
type UploadResult struct {
	Written int `json:"written"`
	Skipped int `json:"skipped"`
}

// UploadInto 把若干条目解包进目标目录。
// onExist：skip=跳过已存在；replace=覆盖；keep_both=自动改名。
func (c *K8sClient) UploadInto(
	ctx context.Context, t FileTarget, destDir string,
	files []UploadFile, onExist string,
) (uploadResult *UploadResult, err error) {
	destDir, err = validatePath(destDir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("没有待上传文件")
	}

	// 先确保目标目录存在。
	if err := c.MakeDir(ctx, t, destDir); err != nil {
		return nil, err
	}

	// 处理同名：keep_both/skip 需要先知道现有条目，查一次目录。
	existing, _ := c.ListFiles(ctx, t, destDir)
	existingSet := make(map[string]bool, len(existing))
	for i := range existing {
		existingSet[existing[i].Name] = true
	}

	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)

	// 归一化相对路径并去 NUL/越界；同名按策略处理。
	type planned struct {
		rel string
		src UploadFile
	}
	plannedFiles := make([]planned, 0, len(files))
	result := &UploadResult{}

	for _, f := range files {
		rel := normalizeRelative(f.RelativePath)
		if rel == "" || strings.HasPrefix(rel, "../") || rel == ".." {
			continue // 越界/空路径丢弃
		}

		// 同名顶层冲突处理（目录/文件）。
		topName := strings.SplitN(rel, "/", 2)[0]
		if existingSet[topName] {
			switch onExist {
			case "skip":
				result.Skipped++
				continue
			case "keep_both":
				rel = uniqueName(rel, existingSet)
			default: // replace：照常写入覆盖
			}
		}
		existingSet[strings.SplitN(rel, "/", 2)[0]] = true
		plannedFiles = append(plannedFiles, planned{rel: rel, src: f})
	}

	if len(plannedFiles) == 0 {
		return result, nil
	}

	// 打 tar 并管道到容器内 tar -x。
	type uploadAudit struct {
		event  oe.Evidence
		before FileState
		path   string
	}
	audits := []uploadAudit{}
	if c.MutationAudit != nil {
		actor, _ := ctx.Value(fileActorKey{}).(string)
		if actor == "" {
			actor = "student"
		}
		for _, f := range plannedFiles {
			dest := path.Join(destDir, f.rel)
			e, aErr := c.MutationAudit.Begin(ctx, t, "upload", dest, "", actor)
			if aErr != nil {
				pr.Close()
				pw.Close()
				return nil, aErr
			}
			audits = append(audits, uploadAudit{e, c.ReadAuditState(ctx, t, dest), dest})
		}
	}
	defer func() {
		for _, a := range audits {
			after := c.ReadAuditState(context.WithoutCancel(ctx), t, a.path)
			if aErr := c.MutationAudit.Finish(ctx, a.event, a.before, after, err); aErr != nil {
				err = ErrFileAuditUnconfirmed
			}
		}
	}()
	go func() {
		var writeErr error
		for _, p := range plannedFiles {
			mode := p.src.Mode
			if mode == 0 {
				mode = 0o644
			}
			hdr := &tar.Header{
				Name: p.rel,
				Mode: mode,
				Size: p.src.Size,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				writeErr = err
				break
			}
			if _, err := io.Copy(tw, p.src.Content); err != nil {
				writeErr = err
				break
			}
		}
		if writeErr != nil {
			_ = tw.Close()
			_ = pw.CloseWithError(writeErr)
			return
		}
		_ = tw.Close()
		_ = pw.Close()
	}()

	_, err = c.runExec(ctx, execOptions{
		sessionID: t.sessionID,
		pod:       t.pod,
		container: t.container,
		// tar -x：从 stdin 解包到目标目录；--no-same-owner 用当前用户。
		command: []string{"tar", "-x", "-C", destDir, "--no-same-owner"},
		stdin:   pr,
	})
	if err != nil {
		return nil, err
	}
	result.Written = len(plannedFiles)
	return result, nil
}

// PreviewDownloadName 只确定下载文件名：单文件=原名，目录=原名.zip。
func (c *K8sClient) PreviewDownloadName(
	ctx context.Context, t FileTarget, p string,
) (string, error) {
	p, err := validatePath(p)
	if err != nil {
		return "", err
	}
	base := path.Base(p)
	isDir, err := c.isDirectory(ctx, t, p)
	if err != nil {
		return "", err
	}
	if isDir {
		return base + ".zip", nil
	}
	return base, nil
}

// StreamDownload 把目标（文件/目录）流式写到 w。
// 单文件原样输出；目录经容器 tar 流实时转 zip（zip 内含一层根目录）。
func (c *K8sClient) StreamDownload(
	ctx context.Context, t FileTarget, p string, w io.Writer,
) error {
	p, err := validatePath(p)
	if err != nil {
		return err
	}
	base := path.Base(p)

	isDir, err := c.isDirectory(ctx, t, p)
	if err != nil {
		return err
	}

	if !isDir {
		_, err := c.runExec(ctx, execOptions{
			sessionID: t.sessionID, pod: t.pod, container: t.container,
			command: []string{"cat", "--", p},
			stdout:  w,
		})
		return err
	}

	// 目录：容器内 tar -c -C parent base，服务端转 zip。
	parent := path.Dir(p)
	pr, pipeW := io.Pipe()

	go func() {
		_, rErr := c.runExec(ctx, execOptions{
			sessionID: t.sessionID, pod: t.pod, container: t.container,
			command: []string{"tar", "-c", "-C", parent, base},
			stdout:  pipeW,
		})
		if rErr != nil {
			_ = pipeW.CloseWithError(rErr)
			return
		}
		_ = pipeW.Close()
	}()

	return tarToZip(pr, w)
}

// isDirectory 在容器内判定路径是否为目录。
func (c *K8sClient) isDirectory(
	ctx context.Context, t FileTarget, p string,
) (bool, error) {
	out, err := c.runExec(ctx, execOptions{
		sessionID: t.sessionID, pod: t.pod, container: t.container,
		command: []string{"/bin/sh", "-c", `if [ -d "$1" ]; then echo 1; else echo 0; fi`, "--", p},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// tarToZip 读 tar 流转写 zip（流式，名称与可执行位保留）。
func tarToZip(r io.Reader, w io.Writer) error {
	tr := tar.NewReader(r)
	zw := zip.NewWriter(w)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar: %w", err)
		}
		fh, err := zip.FileInfoHeader(hdr.FileInfo())
		if err != nil {
			return err
		}
		fh.Name = hdr.Name
		if hdr.FileInfo().IsDir() {
			fh.Name = strings.TrimSuffix(fh.Name, "/") + "/"
		}
		fh.Method = zip.Deflate

		writer, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		if !hdr.FileInfo().IsDir() {
			if _, err := io.Copy(writer, tr); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

// normalizeRelative 规范化相对路径（纯词法），去掉首尾斜杠与 ./、解析 ..。
func normalizeRelative(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	clean := path.Clean("/" + p) // 借绝对路径解析 ..，再去掉前导 /
	return strings.TrimPrefix(clean, "/")
}

// uniqueName 在名称冲突时生成不冲突的新相对路径（name.txt → name (1).txt）。
func uniqueName(rel string, taken map[string]bool) string {
	dir := path.Dir(rel)
	base := path.Base(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; ; i++ {
		candidateBase := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		var candidate string
		if dir == "." || dir == "" {
			candidate = candidateBase
		} else {
			candidate = dir + "/" + candidateBase
		}
		// 仅按顶层名判冲突（与既有 existingSet 口径一致）。
		top := strings.SplitN(candidate, "/", 2)[0]
		if !taken[top] {
			return candidate
		}
	}
}
