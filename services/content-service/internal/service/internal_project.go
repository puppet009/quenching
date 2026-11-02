// 服务间内部查询服务：对编排服务暴露项目/项目服务最小投影，编排层不直接碰库。
package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/quenching/content-service/internal/repository"
)

// InternalProjectService 项目目录内部查询服务。
type InternalProjectService struct {
	reader repository.InternalProjectReader
}

// NewInternalProjectService 创建内部查询服务。
func NewInternalProjectService(reader repository.InternalProjectReader) *InternalProjectService {
	return &InternalProjectService{reader: reader}
}

// BatchProjects 按 ID 列表批量返回项目最小投影（服务端去重）。
func (s *InternalProjectService) BatchProjects(
	ctx context.Context, ids []int64,
) ([]*repository.InternalProjectView, error) {
	uniq, err := normalizeIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(uniq) == 0 {
		return []*repository.InternalProjectView{}, nil
	}
	return s.reader.ListProjectsByIDs(ctx, uniq)
}

// SearchProjectsByName 按名称模糊反查项目。
func (s *InternalProjectService) SearchProjectsByName(
	ctx context.Context, keyword string,
) ([]*repository.InternalProjectView, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	return s.reader.SearchProjectsByName(ctx, kw)
}

// BatchServices 按 ID 列表批量返回项目服务最小投影（服务端去重）。
func (s *InternalProjectService) BatchServices(
	ctx context.Context, ids []int64,
) ([]*repository.InternalProjectServiceView, error) {
	uniq, err := normalizeIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(uniq) == 0 {
		return []*repository.InternalProjectServiceView{}, nil
	}
	return s.reader.ListServicesByIDs(ctx, uniq)
}

// normalizeIDs 过滤非法/重复 ID，防止调用方重复 ID 放大查询。
func normalizeIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
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
	return uniq, nil
}
