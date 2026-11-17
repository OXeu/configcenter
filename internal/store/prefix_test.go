package store

import (
	"crypto/rand"
	"testing"

	"github.com/glebarez/sqlite"
)

func TestListItemsTreatsPrefixWildcardsLiterally(t *testing.T) {
	st, err := Open(t.Context(), sqlite.Open("file:prefix-"+rand.Text()+"?mode=memory&cache=shared"), "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	db, err := st.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	project, err := st.ProjectBySlug(t.Context(), "configcenter")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CC_VALUE", "CCXVALUE", "percent%value", "percentXvalue", "bang!value", "bangvalue", `slash\value`, "slashvalue"} {
		if _, err := st.PutItem(t.Context(), ItemInput{ProjectID: project.ID, EnvironmentID: project.PrimaryEnvironmentID, Key: key, Value: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for prefix, expected := range map[string]string{"CC_": "CC_VALUE", "percent%": "percent%value", "bang!": "bang!value", `slash\`: `slash\value`} {
		t.Run(prefix, func(t *testing.T) {
			items, err := st.ListItems(t.Context(), project.ID, project.PrimaryEnvironmentID, prefix)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Key != expected {
				t.Fatal("prefix did not match exactly one literal key")
			}
		})
	}
}
