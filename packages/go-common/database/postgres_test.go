package database

import "testing"

func TestApplicationConnectionsUseBeijingSessionTimeZone(t *testing.T) {
	cfg, err := poolConfig(PGConfig{DSN: "postgres://user:password@localhost/db?sslmode=disable&timezone=UTC", MaxConns: 7})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.RuntimeParams["timezone"] != "Asia/Shanghai" || cfg.MaxConns != 7 {
		t.Fatalf("session configuration not preserved: %v", cfg.ConnConfig.RuntimeParams)
	}
}
