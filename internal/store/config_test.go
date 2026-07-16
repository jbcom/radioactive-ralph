package store

import (
	"context"
	"testing"
)

func TestSetAndGetProjectConfig(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "config-project")

	if err := s.SetProjectConfig(ctx, projectID, "max_retries", "3"); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}
	if err := s.SetProjectConfig(ctx, projectID, "provider", `"claude"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	cfg, err := s.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if cfg["max_retries"] != "3" || cfg["provider"] != `"claude"` {
		t.Errorf("GetProjectConfig = %+v, want max_retries=3 provider=\"claude\"", cfg)
	}
}

func TestSetProjectConfigUpsert(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "upsert-project")

	if err := s.SetProjectConfig(ctx, projectID, "key", "v1"); err != nil {
		t.Fatalf("SetProjectConfig v1: %v", err)
	}
	if err := s.SetProjectConfig(ctx, projectID, "key", "v2"); err != nil {
		t.Fatalf("SetProjectConfig v2: %v", err)
	}

	cfg, err := s.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if cfg["key"] != "v2" {
		t.Errorf("GetProjectConfig[key] = %q, want v2", cfg["key"])
	}

	var count int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM project_config WHERE project_id = ? AND key = ?", projectID, "key").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("project_config row count = %d, want 1 (upsert, not insert)", count)
	}
}
