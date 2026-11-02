package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"time"
)

func (s *ChatService) WithSessionMemory(store SessionMemoryStore) { s.sessionMemory = store }
func (s *ChatService) RestoreSessionConversation(ctx context.Context, uid, sid int64) (model.SessionRestore, error) {
	if s.sessionMemory == nil || sid <= 0 {
		return model.SessionRestore{}, ErrSessionMemoryNotFound
	}
	c, e := s.sessionMemory.Resolve(ctx, uid, sid)
	if e != nil {
		return model.SessionRestore{}, e
	}
	p, e := s.sessionMemory.History(ctx, uid, sid, 0, 50)
	if e != nil {
		return model.SessionRestore{}, e
	}
	return model.SessionRestore{SessionConversation: *c, SessionHistoryPage: p}, nil
}
func (s *ChatService) SessionConversationHistory(ctx context.Context, uid, sid, before int64, n int) (model.SessionHistoryPage, error) {
	if s.sessionMemory == nil || sid <= 0 || before < 0 {
		return model.SessionHistoryPage{}, ErrSessionMemoryNotFound
	}
	return s.sessionMemory.History(ctx, uid, sid, before, n)
}
func (s *ChatService) beginSessionReply(ctx context.Context, uid, sid int64) (context.Context, func(), error) {
	c, e := s.sessionMemory.Resolve(ctx, uid, sid)
	if e != nil {
		return ctx, nil, e
	}
	if c.SessionStatus != "running" {
		return ctx, nil, ErrConversationClosed
	}
	var token [16]byte
	if _, e = rand.Read(token[:]); e != nil {
		return ctx, nil, e
	}
	l, e := s.sessionMemory.Acquire(ctx, uid, sid, hex.EncodeToString(token[:]), 90*time.Second)
	if e != nil {
		return ctx, nil, e
	}
	runCtx, cancel := context.WithCancel(ctx)
	runCtx = WithChatLease(runCtx, ChatLeaseScope{UserID: uid, SessionID: sid, Lease: l})
	interval := s.chatRenewInterval
	if interval <= 0 {
		interval = 20 * time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				renewCtx, stop := context.WithTimeout(runCtx, 5*time.Second)
				ok, e := s.sessionMemory.Renew(renewCtx, uid, sid, l, 90*time.Second)
				stop()
				if e != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	finish := func() {
		cancel()
		<-done
		releaseCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = s.sessionMemory.Release(releaseCtx, uid, sid, l)
	}
	return runCtx, finish, nil
}
func (s *ChatService) assertSessionReplyLease(ctx context.Context) error {
	scope, ok := CurrentChatLease(ctx)
	if !ok {
		return ctx.Err()
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	ok, e := s.sessionMemory.Renew(ctx, scope.UserID, scope.SessionID, scope.Lease, 90*time.Second)
	if e != nil {
		return e
	}
	if !ok {
		return ErrChatLeaseLost
	}
	return nil
}
func IsSessionConversationMissing(e error) bool {
	return errors.Is(e, ErrSessionMemoryNotFound) || errors.Is(e, ErrConversationNotFound)
}
