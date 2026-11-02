package repository

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/quenching/training-service/internal/service"
)

func (r *BrainRepo) PublicBrainStatus(ctx context.Context, sid, uid int64) (*service.PublicBrainStatus, error) {
	s := &service.PublicBrainStatus{}
	var privateReason string
	err := r.pool.QueryRow(ctx, `SELECT b.phase,b.current_challenge,b.cleanup_status,b.reason,EXISTS(SELECT 1 FROM ability_assessments a WHERE a.session_id=b.session_id) FROM brain_sessions b JOIN training_sessions t ON t.id=b.session_id WHERE b.session_id=$1 AND t.user_id=$2 AND t.deleted_at IS NULL`, sid, uid).Scan(&s.Phase, &s.CurrentChallenge, &s.CleanupStatus, &privateReason, &s.AbilityObserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Status = s.Phase
	if s.Phase == "degraded" {
		s.Reason = "训练调度暂时受限，请稍后重试；具体原因已记录供管理员检查"
	}
	switch privateReason {
	case "no_verified_faults":
		s.Reason = "当前模板尚无已通过验收的可执行故障，等待管理员完成验收"
	case "model unavailable; waiting for a new event":
		s.Reason = "AI 服务暂时不可用，等待恢复后继续训练"
	case "business verification or runtime failed; inspect evidence before retry":
		s.Reason = "交易验证未通过，请检查沙箱服务与业务链路"
	}
	if s.CleanupStatus == "failed" {
		s.Reason = "沙箱清理尚未完成，系统将重试；复盘单独生成"
	}
	return s, nil
}
