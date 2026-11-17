//go:build mysql

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/OXeu/configcenter/internal/store"
)

func TestMySQLStore(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenDatabase(ctx, "mysql", "root@tcp(127.0.0.1:23306)/configcenter?parseTime=true", "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	project, err := st.CreateProject(ctx, store.Project{Slug: "mysql-test", Name: "MySQL"}, store.Environment{Slug: "default", Name: "Default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.DeleteProject(ctx, project.ID); err != nil {
			t.Error(err)
		}
	})
	envs, err := st.ListEnvironments(ctx, project.ID)
	if err != nil || len(envs) != 1 {
		t.Fatalf("environments: %v, %v", envs, err)
	}
	item := store.ItemInput{ProjectID: project.ID, EnvironmentID: envs[0].ID, Key: "key", Value: strings.Repeat("x", 70000)}
	if _, err := st.PutItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	item.Value = "updated"
	if _, err := st.PutItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetItem(ctx, project.ID, envs[0].ID, "key")
	if err != nil || got.Value != "updated" {
		t.Fatalf("get: %v, %v", got, err)
	}
	if err := st.RenameItem(ctx, project.ID, envs[0].ID, "key", "renamed"); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListItems(ctx, project.ID, envs[0].ID, "ren")
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v, %v", items, err)
	}
	if err := st.DeleteItem(ctx, project.ID, envs[0].ID, "renamed"); err != nil {
		t.Fatal(err)
	}
}
