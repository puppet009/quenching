package repository

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/content-service/internal/model"
)

func TestPGProjectFieldsMigrationPreservesHistoryAndSupportsCRUD(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("project_fields_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	apply := func(path string) {
		t.Helper()
		raw, err := os.ReadFile("../../migrations/" + path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	apply("000001_create_projects.up.sql")
	if _, err = pool.Exec(ctx, `INSERT INTO projects(id,name,domain,tech_stack) VALUES(41,'历史项目','ecommerce','["Java"]')`); err != nil {
		t.Fatal(err)
	}
	apply("000006_complete_project_fields.up.sql")
	repo := NewProjectRepo(pool)
	history, err := repo.GetByID(ctx, 41)
	if err != nil || history.Name != "历史项目" || history.Description != "" || history.ArchitectureImage != "" || history.Difficulty != "medium" {
		t.Fatalf("history/defaults: %+v, %v", history, err)
	}
	if !reflect.DeepEqual(history.TechStack, []string{"Java"}) {
		t.Fatalf("historical tech stack lost: %v", history.TechStack)
	}
	project := &model.Project{Name: "真实登记项目", Domain: "ecommerce", Description: "订单训练", TechStack: []string{"Spring"}, ArchitectureImage: "/architecture.png", DeployConfig: model.JSON{}, Difficulty: "easy", Status: "online"}
	if err = repo.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err = repo.Update(ctx, project.ID, map[string]interface{}{"description": "保留修改", "difficulty": "hard"}); err != nil {
		t.Fatal(err)
	}
	// Reapplying the additive migration must preserve admin edits.
	apply("000006_complete_project_fields.up.sql")
	items, total, err := repo.List(ctx, "ecommerce", "online", "hard", "保留修改", 1, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != project.ID || items[0].ArchitectureImage != "/architecture.png" {
		t.Fatalf("list after migration replay: %+v, total=%d, %v", items, total, err)
	}
	if err = repo.Delete(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	items, total, err = repo.List(ctx, "", "", "", "", 1, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != 41 {
		t.Fatalf("CRUD changed historical row: %+v, total=%d, %v", items, total, err)
	}
}
