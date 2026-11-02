package service

import (
	"math/rand"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// AssignRandomGroup 新用户首训前随机分 A/B 组。
// 交叉设计：A 组顺序为 人工->AI，B 组为 AI->人工，消除顺序效应。
// 用注入的随机源便于单测；生产传 rand.New(rand.NewSource(time.Now().UnixNano()))。
func AssignRandomGroup(r *rand.Rand) model.ControlGroup {
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	if r.Intn(2) == 0 {
		return model.GroupA
	}
	return model.GroupB
}
