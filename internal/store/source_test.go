package store

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

func TestConfigurationSourceScopesAndMetadata(t *testing.T) {
	st := sourceStore(t)
	project, err := st.ProjectBySlug(t.Context(), "configcenter")
	if err != nil {
		t.Fatal(err)
	}
	production, err := st.EnvironmentBySlug(t.Context(), project.ID, "production")
	if err != nil {
		t.Fatal(err)
	}
	staging, err := st.CreateEnvironment(t.Context(), Environment{ProjectID: project.ID, Slug: "staging", Name: "Staging"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateProject(t.Context(), Project{Slug: "other", Name: "Other"}, Environment{Slug: "production", Name: "Production"})
	if err != nil {
		t.Fatal(err)
	}
	secret := true
	if err := st.PutItems(t.Context(), []ItemInput{
		{ProjectID: project.ID, EnvironmentID: production.ID, Key: "CC_SECRET", Value: "plaintext", Secret: &secret, Description: "runtime credential"},
		{ProjectID: project.ID, EnvironmentID: production.ID, Key: "CC_EMPTY", Value: ""},
		{ProjectID: project.ID, EnvironmentID: production.ID, Key: "CCX_OTHER", Value: "excluded"},
		{ProjectID: project.ID, EnvironmentID: staging.ID, Key: "CC_SECRET", Value: "staging"},
		{ProjectID: other.ID, EnvironmentID: other.PrimaryEnvironmentID, Key: "CC_SECRET", Value: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := configcenter.LoadSnapshot(t.Context(), "CC_", st.ConfigurationSource("configcenter", "production"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GetDefault("CC_SECRET", "") != "plaintext" || snapshot.GetDefault("CC_EMPTY", "fallback") != "" {
		t.Fatal("database source lost plaintext, scope or empty values")
	}
	items := snapshot.All()
	if len(items) != 2 || items[1].Key != "CC_SECRET" || !items[1].Secret || items[1].Masked || items[1].Description != "runtime credential" || items[1].UpdatedAt.IsZero() {
		t.Fatal("database source lost metadata or did not escape the prefix")
	}
	if err := st.Transaction(t.Context(), func(tx *Store) error {
		if _, err := tx.PutItem(t.Context(), ItemInput{ProjectID: project.ID, EnvironmentID: production.ID, Key: "CC_EMPTY", Value: "uncommitted"}); err != nil {
			return err
		}
		view, err := configcenter.LoadSnapshot(t.Context(), "CC_", tx.ConfigurationSource("configcenter", "production"))
		if err != nil {
			return err
		}
		if view.GetDefault("CC_EMPTY", "") != "uncommitted" {
			return errors.New("source did not reuse the transaction connection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationSourceErrors(t *testing.T) {
	st := sourceStore(t)
	for _, scope := range [][2]string{{"missing", "production"}, {"configcenter", "missing"}} {
		_, err := configcenter.LoadSnapshot(t.Context(), "", st.ConfigurationSource(scope[0], scope[1]))
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing scope did not return not found: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := st.ConfigurationSource("configcenter", "production").Read(ctx, "CC_"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled database read, got %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfigurationSource("configcenter", "production").Read(t.Context(), "CC_"); err == nil {
		t.Fatal("failed database read was silently replaced by an empty snapshot")
	}
}

func sourceStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenDatabase(t.Context(), "sqlite", "file:source-"+rand.Text()+"?mode=memory&cache=shared", "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	return st
}
