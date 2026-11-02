package service

import (
	"context"
	"fmt"
)

// ResourceChecker 集群资源检查接口。
// 抽象出来便于单测 mock——不依赖真实 K8s 集群也能验证业务逻辑。
type ResourceChecker interface {
	// HasEnoughResource 检查集群剩余资源是否足够创建一个沙箱。
	// 足够返回 true，不足返回 false；出错时返回错误。
	HasEnoughResource(ctx context.Context) (bool, error)
}

// k8sResourceChecker 基于真实 K8s 集群的资源检查实现。
type k8sResourceChecker struct {
	client *K8sClient
}

func (c *k8sResourceChecker) HasEnoughResource(ctx context.Context) (bool, error) {
	res, err := c.client.GetClusterAllocatable(ctx)
	if err != nil {
		return false, fmt.Errorf("检查集群资源失败: %w", err)
	}
	return res.HasEnough(), nil
}

// NewK8sResourceChecker 用 K8s 客户端构造资源检查器。
func NewK8sResourceChecker(client *K8sClient) ResourceChecker {
	return &k8sResourceChecker{client: client}
}
