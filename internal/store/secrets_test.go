package store

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSecretMigrationAndWrites(t *testing.T) {
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
	if err := db.Exec("CREATE TABLE config_items (id integer PRIMARY KEY, project_id integer NOT NULL, environment_id integer NOT NULL, key text NOT NULL, value text NOT NULL, description text, updated_by integer NOT NULL, created_at datetime NOT NULL, updated_at datetime NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO config_items VALUES (1, 1, 1, 'legacy', 'original', '', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), dialect, "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.GetItem(t.Context(), 1, 1, "legacy")
	if err != nil || legacy.Secret || legacy.Value != "original" {
		t.Fatal("migration changed existing configuration", err)
	}
	yes, no := true, false
	for _, secret := range []*bool{&yes, nil, &no, nil} {
		item, err := s.PutItem(t.Context(), ItemInput{ProjectID: 1, EnvironmentID: 1, Key: "legacy", Value: "updated", Secret: secret, UpdatedBy: 1})
		if err != nil {
			t.Fatal(err)
		}
		if secret != nil {
			legacy.Secret = *secret
		}
		if item.Secret != legacy.Secret || item.Value != "updated" {
			t.Fatal("write did not preserve or explicitly change the secret flag")
		}
	}
}

func TestSecretBatchAndProposalRollback(t *testing.T) {
	s, err := Open(t.Context(), sqlite.Open(":memory:"), "admin@example.com", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	value := "original"
	proposal, err := s.CreateProposal(t.Context(), MCPProposal{ProjectID: 1, EnvironmentID: 1, Key: "token", Value: &value, Action: ActionSet, Status: StatusPending})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("CREATE TRIGGER reject_protection BEFORE UPDATE OF secret ON mcp_proposals BEGIN SELECT RAISE(ABORT, 'rejected protection'); END").Error; err != nil {
		t.Fatal(err)
	}
	yes := true
	items := []ItemInput{{ProjectID: 1, EnvironmentID: 1, Key: "public", Value: "public"}, {ProjectID: 1, EnvironmentID: 1, Key: "token", Value: value, Secret: &yes}}
	if err := s.PutItems(t.Context(), items); err == nil {
		t.Fatal("partially protected batch committed")
	}
	got, err := s.ListItems(t.Context(), 1, 1, "")
	if err != nil || len(got) != 0 {
		t.Fatal("failed protection did not roll back the entire batch", err)
	}
	if err := s.db.Exec("DROP TRIGGER reject_protection").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.PutItems(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	history, err := s.ProposalByID(t.Context(), 1, proposal.ID)
	if err != nil || !history.Secret {
		t.Fatal("existing proposal was not protected", err)
	}
	inherited, err := s.CreateProposal(t.Context(), MCPProposal{ProjectID: 1, EnvironmentID: 1, Key: "token", Value: &value, Action: ActionSet, Status: StatusPending})
	if err != nil || !inherited.Secret {
		t.Fatal("new proposal did not inherit protection", err)
	}
	if _, err := s.ReviewProposal(t.Context(), 1, inherited.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	item, err := s.GetItem(t.Context(), 1, 1, "token")
	if err != nil || !item.Secret || item.Value != value {
		t.Fatal("proposal application lost the secret", err)
	}
	target, err := s.CreateProposal(t.Context(), MCPProposal{ProjectID: 1, EnvironmentID: 1, Key: "renamed", Value: &value, Action: ActionSet, Status: StatusPending})
	if err != nil || target.Secret {
		t.Fatal("unexpected protection before rename", err)
	}
	if err := s.RenameItem(t.Context(), 1, 1, "token", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(t.Context(), 1, 1, "renamed"); err != nil {
		t.Fatal(err)
	}
	target, err = s.ProposalByID(t.Context(), 1, target.ID)
	if err != nil || !target.Secret {
		t.Fatal("rename and deletion lost target proposal protection", err)
	}
}
