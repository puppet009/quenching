package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/sandbox-service/internal/model"
	"reflect"
	"regexp"
)

var behaviorCommandID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func ValidateBehaviorCommand(sid int64, c model.BehaviorCommand) error {
	if c.SessionID != sid || sid <= 0 || !behaviorCommandID.MatchString(c.CommandID) || c.TemplateSlug == "" {
		return errors.New("invalid command scope")
	}
	switch c.Operation {
	case "baseline", "register", "login", "browse", "cart", "order", "pay", "verify":
	default:
		return errors.New("unsupported operation")
	}
	var p map[string]any
	if json.Unmarshal([]byte(c.ParametersJSON), &p) != nil || p == nil {
		return errors.New("parameters must be an object")
	}
	for key := range p {
		switch key {
		case "profile", "concurrency", "duration_seconds":
		default:
			return errors.New("unexpected behavior parameter")
		}
	}
	return nil
}
func SameBehaviorCommand(a, b model.BehaviorCommand) bool {
	var x, y any
	if json.Unmarshal([]byte(a.ParametersJSON), &x) != nil || json.Unmarshal([]byte(b.ParametersJSON), &y) != nil {
		return false
	}
	return a.CommandID == b.CommandID && a.SessionID == b.SessionID && a.TemplateSlug == b.TemplateSlug && a.Operation == b.Operation && reflect.DeepEqual(x, y)
}
func BehaviorCommandMayRepeat(status string) bool { return status == "accepted" }

type BehaviorCommandStore interface {
	InsertCommand(context.Context, model.BehaviorCommand) (model.BehaviorResult, error)
	GetCommand(context.Context, int64, string) (model.BehaviorResult, error)
	ClaimCommand(context.Context, int64) (*model.BehaviorCommand, error)
	FinishCommand(context.Context, int64, model.BehaviorResult) error
	RunnerTokenHash(context.Context, int64) (string, error)
}
type BehaviorPreparer interface {
	PrepareBehavior(context.Context, model.BehaviorCommand) error
}
type BehaviorCommandService struct {
	store   BehaviorCommandStore
	prepare BehaviorPreparer
}

func NewBehaviorCommandService(store BehaviorCommandStore, prepare BehaviorPreparer) *BehaviorCommandService {
	return &BehaviorCommandService{store, prepare}
}

var ErrBehaviorCommandRejected = errors.New("behavior command rejected before reservation")

func (s *BehaviorCommandService) Submit(ctx context.Context, c model.BehaviorCommand) (out model.BehaviorResult, retErr error) {
	insertAttempted := false
	defer func() {
		if retErr != nil && !insertAttempted {
			retErr = fmt.Errorf("%w: %v", ErrBehaviorCommandRejected, retErr)
		}
	}()
	if err := ValidateBehaviorCommand(c.SessionID, c); err != nil {
		return model.BehaviorResult{}, err
	}
	existing, err := s.store.GetCommand(ctx, c.SessionID, c.CommandID)
	if err == nil && existing.CommandID != "" {
		insertAttempted = true
		return s.store.InsertCommand(ctx, c)
	}
	if s.prepare == nil {
		return model.BehaviorResult{}, errors.New("behavior runtime unavailable")
	}
	if err = s.prepare.PrepareBehavior(ctx, c); err != nil {
		return model.BehaviorResult{}, err
	}
	insertAttempted = true
	return s.store.InsertCommand(ctx, c)
}
func (s *BehaviorCommandService) Get(ctx context.Context, sid int64, id string) (model.BehaviorResult, error) {
	return s.store.GetCommand(ctx, sid, id)
}
func (s *BehaviorCommandService) AuthenticateRunner(ctx context.Context, sid int64, token string) error {
	if token == "" {
		return errors.New("runner authorization required")
	}
	expected, err := s.store.RunnerTokenHash(ctx, sid)
	if err != nil || expected == "" {
		return errors.New("runner authorization unavailable")
	}
	sum := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(hex.EncodeToString(sum[:]))) != 1 {
		return errors.New("invalid runner authorization")
	}
	return nil
}
func (s *BehaviorCommandService) Claim(ctx context.Context, sid int64) (*model.BehaviorCommand, error) {
	return s.store.ClaimCommand(ctx, sid)
}
func (s *BehaviorCommandService) Finish(ctx context.Context, sid int64, r model.BehaviorResult) error {
	if !behaviorCommandID.MatchString(r.CommandID) || (r.Status != "succeeded" && r.Status != "failed" && r.Status != "unknown") {
		return errors.New("invalid runner result")
	}
	var payload map[string]any
	if json.Unmarshal([]byte(r.ResultJSON), &payload) != nil || payload == nil {
		return errors.New("invalid result payload")
	}
	return s.store.FinishCommand(ctx, sid, r)
}
