package operationevidence

import "time"

func EvaluateSeal(s AuditSeal, now time.Time) AuditSeal {
	if s.State == "timed_out" {
		return s
	}
	s.State = "pending"
	if s.Pending == 0 && s.Acknowledged >= s.Watermark {
		s.State = "ready"
	} else if !now.Before(s.Deadline) {
		s.State = "timed_out"
		s.Gaps = append(s.Gaps, "audit_delivery_timeout")
	}
	return s
}
