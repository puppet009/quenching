package service

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient 对 K8s 操作的薄封装。可注入真实 clientset 或 fake clientset（单测用）。
// restCfg 仅在连真实集群时非空，用于建立 kubectl exec（SPDY）通道；
// fake clientset 下为 nil，exec 类操作返回明确错误。
type K8sClient struct {
	MutationAudit        *FileAudit
	clientset            kubernetes.Interface
	restCfg              *rest.Config
	faultReaderNamespace string
	faultReaderAccount   string
	faultReaderEnabled   bool
	terminalNamespace    string
	terminalClusterRole  string
}

// NewK8sClient 用已有 clientset 构造（单测注入 fake，生产注入真实）。
func NewK8sClient(cs kubernetes.Interface) *K8sClient {
	return &K8sClient{clientset: cs}
}

// NewK8sClientFromKubeconfig 用 kubeconfig 构造真实客户端（集群外调试用）。
func NewK8sClientFromKubeconfig(kubeconfigPath string) (*K8sClient, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &K8sClient{clientset: cs, restCfg: restCfg}, nil
}

// NewK8sClientInCluster 用集群内 ServiceAccount 构造真实客户端（云上 Pod 默认路径）。
// 不在集群内运行时返回 rest.ErrNotInCluster，调用方据此决定是否降级 fake。
func NewK8sClientInCluster() (*K8sClient, error) {
	restCfg, err := restInClusterConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &K8sClient{clientset: cs, restCfg: restCfg}, nil
}

// Clientset 暴露底层接口，供调谐器等需要更多操作的地方使用。
func (c *K8sClient) Clientset() kubernetes.Interface { return c.clientset }

// RealCluster 判断是否连接真实集群（持有 rest.Config，可走 SPDY exec）。
// fake clientset 本地降级时返回 false，供行为模拟等运行时决定是否真正启动。
func (c *K8sClient) RealCluster() bool { return c.restCfg != nil }

func isAlreadyExists(err error) bool { return errors.IsAlreadyExists(err) }
func isNotFound(err error) bool      { return errors.IsNotFound(err) }
