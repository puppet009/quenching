// 服务间剧本详情查询服务：含答案列 root_cause，仅供 training 复盘聚合服务端调用。
// 与脱敏内部查询服务分离，便于在路由/网络层对该能力做更严格的来源限制。
package service

import (
	"context"

	"github.com/quenching/content-service/internal/repository"
)

// InternalDefectScriptService 剧本详情内部查询服务。
type InternalDefectScriptService struct {
	reader repository.InternalDefectScriptReader
}

// NewInternalDefectScriptService 创建剧本详情查询服务。
func NewInternalDefectScriptService(reader repository.InternalDefectScriptReader) *InternalDefectScriptService {
	return &InternalDefectScriptService{reader: reader}
}

// ScriptDetails 按 ID 列表批量返回剧本详情（去重、过滤非法 ID）。
func (s *InternalDefectScriptService) ScriptDetails(ctx context.Context, ids []int64) ([]*repository.InternalDefectScriptView, error) {
	seen := make(map[int64]struct{}, len(ids))
	uniq := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return []*repository.InternalDefectScriptView{}, nil
	}
	return s.reader.ListScriptsByIDs(ctx, uniq)
}
