package service

import (
	"context"
	"github.com/quenching/training-service/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
)

type commandLookupFixture struct{}

func (commandLookupFixture) LookupCommand(context.Context, string) (*model.BrainCommand, error) {
	return &model.BrainCommand{CommandID: "c1", SessionID: 7, Domain: "sandbox"}, nil
}
func TestBrainDomainQueryScopeAndIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/sandboxes/7/behavior/commands/c1" || r.Header.Get("X-Internal-Token") != "internal" {
			t.Error("scope or credentials lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"command_id":"foreign","status":"succeeded","result_json":"{}"}}`))
	}))
	defer server.Close()
	c := NewBrainDomainClients(server.URL, server.URL, "internal", commandLookupFixture{})
	if _, err := c.GetCommand(context.Background(), "sandbox", "c1"); err == nil {
		t.Fatal("foreign result accepted")
	}
	if _, err := c.GetCommand(context.Background(), "https://external.example", "c1"); err == nil {
		t.Fatal("external domain accepted")
	}
}

func TestDefinitiveDomainRejectionIsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"code":14009,"command_rejected":true}`))
	}))
	defer server.Close()
	c := NewBrainDomainClients(server.URL, server.URL, "internal", commandLookupFixture{})
	result, err := c.Submit(context.Background(), model.BrainCommand{CommandID: "c1", SessionID: 7, Domain: "sandbox"}, map[string]any{})
	if err == nil || result.Status != "failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestAmbiguousDomainErrorRemainsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer server.Close()
	c := NewBrainDomainClients(server.URL, server.URL, "internal", commandLookupFixture{})
	result, _ := c.Submit(context.Background(), model.BrainCommand{CommandID: "c1", SessionID: 7, Domain: "sandbox"}, nil)
	if result.Status != "unknown" {
		t.Fatal("ambiguous mutation made replayable")
	}
}
