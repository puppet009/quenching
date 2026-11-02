package service

import "k8s.io/client-go/rest"

// restInClusterConfig 单独封装，便于无集群环境下编译（该函数无需真集群即可编译通过）。
func restInClusterConfig() (*rest.Config, error) {
	return rest.InClusterConfig()
}
