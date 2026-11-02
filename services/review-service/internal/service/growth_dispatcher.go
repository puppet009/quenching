package service

import (
	"context"
	"time"
)

type GrowthDispatcher struct {
	store    GrowthJobStore
	producer ReviewProducer
}

func NewGrowthDispatcher(store GrowthJobStore, producer ReviewProducer) *GrowthDispatcher {
	return &GrowthDispatcher{store: store, producer: producer}
}
func (d *GrowthDispatcher) Dispatch(ctx context.Context) error {
	ids, err := d.store.Due(ctx, 32)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if e := d.producer.Publish(ctx, &ReviewTask{ReviewID: id}); e != nil && first == nil {
			first = e
		}
	}
	return first
}
func (d *GrowthDispatcher) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.Dispatch(ctx); err != nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
