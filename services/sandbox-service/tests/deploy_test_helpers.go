package tests

import (
	"context"
	"errors"
	"sync"

	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/service"
)

var errFakeInstallFail = errors.New("fake install error")

// fakeRegistry 模拟模板注册表，用于单测。
type fakeRegistry struct {
	templates map[string]templates.Manifest
}

// newFakeRegistry 用给定 slug 构造含对应假模板的注册表。
func newFakeRegistry(slugs ...string) *fakeRegistry {
	r := &fakeRegistry{templates: make(map[string]templates.Manifest)}
	for _, slug := range slugs {
		r.templates[slug] = templates.Manifest{
			Slug:  slug,
			Name:  slug,
			Chart: "helm",
		}
	}
	return r
}

func (r *fakeRegistry) List() []templates.Manifest {
	out := make([]templates.Manifest, 0, len(r.templates))
	for _, m := range r.templates {
		out = append(out, m)
	}
	return out
}

func (r *fakeRegistry) Get(slug string) (templates.Manifest, bool) {
	m, ok := r.templates[slug]
	return m, ok
}

func (r *fakeRegistry) ChartPath(m templates.Manifest) string {
	return "/charts/" + m.Slug
}

// fakeDeployer 模拟部署器，用于单测。
type fakeDeployer struct {
	mu        sync.Mutex
	installed map[string]bool // key: namespace/release
	// 控制 Install 是否失败
	installError error
	// 记录调用次数
	installCount   int
	uninstallCount int
}

func newFakeDeployer() *fakeDeployer {
	return &fakeDeployer{
		installed: make(map[string]bool),
	}
}

func (f *fakeDeployer) Install(ctx context.Context, chart service.ChartInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installCount++
	if f.installError != nil {
		return f.installError
	}
	key := chart.Namespace + "/" + chart.Name
	f.installed[key] = true
	return nil
}

func (f *fakeDeployer) Uninstall(ctx context.Context, namespace, release string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uninstallCount++
	key := namespace + "/" + release
	delete(f.installed, key)
	return nil
}

func (f *fakeDeployer) Exists(ctx context.Context, namespace, release string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := namespace + "/" + release
	return f.installed[key], nil
}

// fakeWaiter 模拟就绪等待器。
type fakeWaiter struct {
	readyCount int
	waitError  error
	callCount  int
}

func (f *fakeWaiter) WaitForDeployments(ctx context.Context, namespace string) (int, error) {
	f.callCount++
	if f.waitError != nil {
		return 0, f.waitError
	}
	return f.readyCount, nil
}
