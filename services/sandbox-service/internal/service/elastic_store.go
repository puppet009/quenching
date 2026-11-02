package service

import (
	"context"
	"github.com/quenching/sandbox-service/internal/model"
)

type ElasticStore interface {
	WithElasticLock(context.Context, bool, func(context.Context) error) error
	ElasticState(context.Context) (model.ElasticState, error)
	InsertElasticPolicy(context.Context, model.ElasticPolicy) error
	SaveElasticPolicy(context.Context, *model.ElasticPolicy, *model.ResourcePlan) error
	AcquireScalingHold(context.Context, model.ScalingHold) error
	ReleaseScalingHold(context.Context, string, string) error
	DeleteElasticNamespace(context.Context, string) error
	AuditElastic(context.Context, model.ElasticAudit) error
	WithAllocationLock(context.Context, func(context.Context) error) error
	ListReservations(context.Context) ([]model.Sandbox, error)
	GetBySessionID(context.Context, int64) (*model.Sandbox, error)
}
