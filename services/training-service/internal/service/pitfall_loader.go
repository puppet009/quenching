package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/quenching/training-service/internal/model"
)

// Store 坑点配置读取端口。PG 实现在 internal/repository，纯逻辑用内存 fake 单测。
type Store interface {
	// ListActiveByDefectIDs 取多个缺陷下所有 status='active' 的坑点配置。
	// 行顺序不作保证，由加载器统一排序；defectIDs 为空时返回空结果且不报错。
	ListActiveByDefectIDs(ctx context.Context, defectIDs []int64) ([]model.PitfallStoredRow, error)
}

// Loader 训练启动时加载坑点配置：按缺陷读取 active 配置 -> 解析三块 JSONB
// -> 强类型校验 -> 按缺陷分组并排序。纯逻辑，不依赖数据库。
type Loader struct {
	store Store
}

func NewLoader(store Store) *Loader {
	return &Loader{store: store}
}

// LoadByDefects 加载给定缺陷集合关联的全部 active 坑点。
//
// 返回 map[defectID][]*model.PitfallConfig，组内按 sort_order、id 升序（与后台配置的触发顺序一致）。
// 策略：严格 fail-fast——任一 active 配置解析或校验失败即整体返回错误并带配置ID，
// 宁可训练启动前暴露配置问题，也不在训练中静默丢坑（会导致人机对照实验数据污染）。
// defectIDs 为空（纯人工/无坑故障）时返回空 map。
func (l *Loader) LoadByDefects(ctx context.Context, defectIDs []int64) (map[int64][]*model.PitfallConfig, error) {
	result := map[int64][]*model.PitfallConfig{}
	if len(defectIDs) == 0 {
		return result, nil
	}

	// 去重，避免同一缺陷被重复传入导致冗余查询。
	uniq := dedup(defectIDs)

	rows, err := l.store.ListActiveByDefectIDs(ctx, uniq)
	if err != nil {
		return nil, fmt.Errorf("加载坑点配置失败: %w", err)
	}

	seen := map[int64]bool{}
	for i := range rows {
		row := &rows[i]

		// 防御性过滤：即便 SQL 已限定 active，再拦一道草稿/下线配置。
		if row.Status != "" && row.Status != model.StatusActive {
			continue
		}
		if seen[row.ID] {
			// 同一配置不应因任何原因出现两次；去重而非重复注入。
			continue
		}
		seen[row.ID] = true

		// ParsePitfallStoredRow 内部已做缺省归一与完整 Validate，无需二次校验。
		cfg, err := model.ParsePitfallStoredRow(row)
		if err != nil {
			return nil, err
		}
		result[cfg.DefectID] = append(result[cfg.DefectID], cfg)
	}

	for defectID := range result {
		cfgs := result[defectID]
		sort.Slice(cfgs, func(i, j int) bool {
			if cfgs[i].SortOrder != cfgs[j].SortOrder {
				return cfgs[i].SortOrder < cfgs[j].SortOrder
			}
			return cfgs[i].ID < cfgs[j].ID
		})
		result[defectID] = cfgs
	}
	return result, nil
}

func dedup(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
