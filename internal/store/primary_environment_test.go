package store

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPrimaryEnvironmentMigration(t *testing.T) {
	dialect := sqlite.Open("file:" + t.Name() + "?mode=memory&cache=shared")
	db, err := gorm.Open(dialect, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("CREATE TABLE projects (id integer PRIMARY KEY, owner_id integer NOT NULL DEFAULT 0, slug text NOT NULL, name text NOT NULL, description text, created_at datetime NOT NULL, updated_at datetime NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"legacy", "custom", "empty"} {
		if err := db.Exec("INSERT INTO projects (owner_id, slug, name, created_at, updated_at) VALUES (1, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", slug, slug).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&Environment{}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []Environment{{ProjectID: 1, Slug: "alpha", Name: "Alpha"}, {ProjectID: 1, Slug: "production", Name: "Production"}, {ProjectID: 2, Slug: "staging", Name: "Staging"}, {ProjectID: 2, Slug: "alpha", Name: "Alpha"}} {
		if err := db.Create(&env).Error; err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(t.Context(), dialect, "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ project, slug string }{{"legacy", "production"}, {"custom", "staging"}, {"configcenter", "production"}} {
		project, err := s.ProjectBySlug(t.Context(), tc.project)
		if err != nil {
			t.Fatal(err)
		}
		env, err := s.EnvironmentBySlug(t.Context(), project.ID, tc.slug)
		if err != nil || project.PrimaryEnvironmentID != env.ID {
			t.Fatal("incorrect migrated primary", project, env, err)
		}
	}
	legacy, err := s.ProjectBySlug(t.Context(), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := s.EnvironmentBySlug(t.Context(), legacy.ID, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimaryEnvironment(t.Context(), legacy.ID, alpha.ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), dialect, "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = reopened.ProjectBySlug(t.Context(), "legacy")
	if err != nil || legacy.PrimaryEnvironmentID != alpha.ID {
		t.Fatal("restart overwrote primary selection", err)
	}
	empty, err := s.ProjectBySlug(t.Context(), "empty")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateEnvironment(t.Context(), Environment{ProjectID: empty.ID, Slug: "first", Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err = s.ProjectBySlug(t.Context(), "empty")
	if err != nil || empty.PrimaryEnvironmentID != first.ID {
		t.Fatal("first environment is not primary", err)
	}
}

func TestPrimaryEnvironmentIsolationAndDeletion(t *testing.T) {
	s, err := Open(t.Context(), sqlite.Open(":memory:"), "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(t.Context(), Project{Slug: "app", Name: "App"}, Environment{Slug: "production", Name: "Production"})
	if err != nil {
		t.Fatal(err)
	}
	production, err := s.EnvironmentBySlug(t.Context(), project.ID, "production")
	if err != nil || project.PrimaryEnvironmentID != production.ID {
		t.Fatal("new project primary missing", err)
	}
	staging, err := s.CreateEnvironment(t.Context(), Environment{ProjectID: project.ID, Slug: "staging", Name: "Staging"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimaryEnvironment(t.Context(), 1, staging.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-project primary accepted", err)
	}
	if err := s.DeleteEnvironment(t.Context(), 1, staging.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-project deletion accepted", err)
	}
	if err := s.RenameEnvironment(t.Context(), 1, staging.ID, "renamed", "Renamed"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-project rename accepted", err)
	}
	for range 2 {
		if err := s.SetPrimaryEnvironment(t.Context(), project.ID, staging.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RenameEnvironment(t.Context(), project.ID, staging.ID, "preview", "Preview"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutItem(t.Context(), ItemInput{ProjectID: project.ID, EnvironmentID: staging.ID, Key: "keep", Value: "value"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnvironment(t.Context(), project.ID, staging.ID); !errors.Is(err, ErrPrimaryEnvironment) {
		t.Fatal("primary deletion accepted", err)
	}
	if _, err := s.GetItem(t.Context(), project.ID, staging.ID, "keep"); err != nil {
		t.Fatal("rejected deletion removed configuration", err)
	}
	if err := s.DeleteEnvironment(t.Context(), project.ID, production.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPrimaryEnvironment(t.Context(), project.ID, production.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted primary accepted", err)
	}
	project, err = s.ProjectBySlug(t.Context(), "app")
	if err != nil || project.PrimaryEnvironmentID != staging.ID {
		t.Fatal("primary lost after rename or deletion", err)
	}
	if err := s.SetPrimaryEnvironment(t.Context(), project.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("empty primary accepted", err)
	}
}
