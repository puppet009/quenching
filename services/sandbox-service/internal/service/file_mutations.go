package service

import (
	"context"
)

func (c *K8sClient) WriteFile(ctx context.Context, t FileTarget, p string, content []byte) error {
	fn := func() error { return c.unauditedWriteFile(ctx, t, p, content) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "writefile", p, "", fn)
}
func (c *K8sClient) MakeDir(ctx context.Context, t FileTarget, p string) error {
	fn := func() error { return c.unauditedMakeDir(ctx, t, p) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "makedir", p, "", fn)
}
func (c *K8sClient) MakeFile(ctx context.Context, t FileTarget, p string) error {
	fn := func() error { return c.unauditedMakeFile(ctx, t, p) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "makefile", p, "", fn)
}
func (c *K8sClient) Rename(ctx context.Context, t FileTarget, from, to string) error {
	fn := func() error { return c.unauditedRename(ctx, t, from, to) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "rename", from, to, fn)
}
func (c *K8sClient) Copy(ctx context.Context, t FileTarget, from, to string) error {
	fn := func() error { return c.unauditedCopy(ctx, t, from, to) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "copy", from, to, fn)
}
func (c *K8sClient) Chmod(ctx context.Context, t FileTarget, p, mode string) error {
	fn := func() error { return c.unauditedChmod(ctx, t, p, mode) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "chmod", p, "", fn)
}
func (c *K8sClient) Remove(ctx context.Context, t FileTarget, p string) error {
	fn := func() error { return c.unauditedRemove(ctx, t, p) }
	if c.MutationAudit == nil {
		return fn()
	}
	return c.MutationAudit.Do(ctx, t, "remove", p, "", fn)
}
