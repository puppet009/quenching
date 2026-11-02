package service

import (
	"context"
	"github.com/quenching/content-service/internal/model"
)

type Store interface {
	ListActiveByDefectIDs(context.Context, []int64) ([]model.PitfallStoredRow, error)
}
