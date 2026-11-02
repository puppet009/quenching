package service

import "github.com/quenching/go-common/response"

// 沙箱域错误码段位：3xxxx。
var (
	ErrSandboxNotFound             = response.New(30004, "沙箱不存在")
	ErrSandboxConflict             = response.New(30005, "沙箱状态冲突")
	ErrSandboxQuotaExceeded        = response.New(30006, "沙箱配额超限")
	ErrSandboxInsufficientResource = response.New(30007, "集群资源不足，请稍后再试")
	ErrSandboxTemplateNotFound     = response.New(30008, "模板不存在或未启用")
	ErrSandboxProjectRequired      = response.New(30009, "必须指定训练项目（project_id 或 template_slug）")
)

// quotaExceededErr 构造配额超限错误（带动态数量信息）。
func quotaExceededErr(current, limit int) *response.Error {
	return response.New(30006,
		"沙箱配额超限：同一用户仅允许同时运行 "+int2str(limit)+" 个沙箱，当前已有 "+int2str(current)+" 个")
}

// int2str 简易 int->string，避免引入 strconv 依赖（与 model 包保持一致风格）。
func int2str(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
