package operationevidence

import (
	"encoding/json"
	"errors"
	"strings"
)

func Validate(e Evidence) error {
	if e.SchemaVersion != 1 || e.EventID == "" || e.OperationID == "" || e.SessionID <= 0 || e.Sequence <= 0 {
		return errors.New("invalid operation identity")
	}
	if e.Actor != "student" && e.Actor != "assistant" && e.Actor != "platform" {
		return errors.New("invalid operation actor")
	}
	if e.Status != "pending" && e.Status != "running" && !Terminal(e.Status) {
		return errors.New("invalid operation status")
	}
	if e.Kind != "command" && e.Kind != "tool" && e.Kind != "file" && e.Kind != "terminal" {
		return errors.New("invalid operation kind")
	}
	if len(e.Command) > 16<<10 || len(e.Arguments) > TextLimit || len(e.Result) > TextLimit {
		return errors.New("operation payload too large")
	}
	if e.Status == "unknown" && e.ExitCode != nil {
		return errors.New("unknown result has exit code")
	}
	for _, raw := range []json.RawMessage{e.Arguments, e.Result} {
		if len(raw) > 0 && !json.Valid(raw) {
			return errors.New("invalid JSON payload")
		}
	}
	for _, s := range []string{e.Coverage.Command, e.Coverage.Execution, e.Coverage.Output, e.Coverage.Before, e.Coverage.After} {
		if s != "" && s != "complete" && s != "partial" && s != "unknown" && s != "unavailable" {
			return errors.New("invalid coverage")
		}
	}
	for _, ref := range e.TextRefs {
		if !strings.HasPrefix(ref, "optext:"+e.OperationID+":") {
			return errors.New("foreign text reference")
		}
	}
	return nil
}
