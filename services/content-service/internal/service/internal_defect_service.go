// 服务间内部查询服务：对编排服务暴露脱敏故障目录，编排层不直接碰库。
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/quenching/content-service/internal/repository"
)

// InternalDefectService 故障剧本内部查询服务。
type InternalDefectService struct {
	reader repository.InternalDefectReader
}

// NewInternalDefectService 创建内部查询服务。
func NewInternalDefectService(reader repository.InternalDefectReader) *InternalDefectService {
	return &InternalDefectService{reader: reader}
}

// BatchDefects 按 ID 列表批量返回脱敏投影。
func (s *InternalDefectService) BatchDefects(ctx context.Context, ids []int64) ([]*repository.InternalDefectView, error) {
	if len(ids) == 0 {
		return []*repository.InternalDefectView{}, nil
	}
	// 去重，防止调用方重复 ID 放大查询与结果。
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
		return []*repository.InternalDefectView{}, nil
	}
	return s.reader.ListByIDs(ctx, uniq)
}

// ProjectDefects 返回项目下启用中的故障目录。
func (s *InternalDefectService) ProjectDefects(ctx context.Context, projectID int64) ([]*repository.InternalDefectView, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("项目ID非法")
	}
	return s.reader.ListActiveByProject(ctx, projectID)
}

// SearchDefectByName 按名称模糊反查故障 ID/名称。
func (s *InternalDefectService) SearchDefectByName(ctx context.Context, keyword string) ([]*repository.InternalDefectName, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	return s.reader.SearchByName(ctx, strings.TrimSpace(keyword))
}
