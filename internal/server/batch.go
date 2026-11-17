package server

import (
	"fmt"
	"net/http"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/slug"
	"github.com/OXeu/configcenter/internal/store"
	"github.com/gin-gonic/gin"
)

func validateBatchKeys(keys []string) error {
	if len(keys) == 0 || len(keys) > 500 {
		return fmt.Errorf("%w: keys must contain 1 to 500 entries", errBadRequest)
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if err := slug.ValidateKey(key); err != nil {
			return fmt.Errorf("key %q: %w", key, err)
		}
		if seen[key] {
			return fmt.Errorf("%w: duplicate key %q", errBadRequest, key)
		}
		seen[key] = true
	}
	return nil
}

func (s *Server) readItems(c *gin.Context, project store.Project, env store.Environment) {
	keys, selected := c.Request.URL.Query()["keys"]
	if !selected {
		items, err := s.store.ListItems(c.Request.Context(), project.ID, env.ID, c.Query("prefix"))
		if err != nil {
			s.fail(c, err)
			return
		}
		s.itemsResponse(c, project, env, items)
		return
	}
	if c.Request.URL.Query().Has("prefix") {
		s.fail(c, fmt.Errorf("%w: keys and prefix cannot be combined", errBadRequest))
		return
	}
	if err := validateBatchKeys(keys); err != nil {
		s.fail(c, err)
		return
	}
	items, err := s.store.GetItems(c.Request.Context(), project.ID, env.ID, keys)
	if err != nil {
		s.fail(c, err)
		return
	}
	out := make([]itemJSON, 0, len(items))
	found := make(map[string]bool, len(items))
	for _, item := range items {
		out = append(out, toItemJSON(c, item))
		found[item.Key] = true
	}
	missing := []string{}
	for _, key := range keys {
		if !found[key] {
			missing = append(missing, key)
		}
	}
	c.JSON(http.StatusOK, gin.H{"project": project.Slug, "environment": env.Slug, "items": out, "missing_keys": missing})
}

func (s *Server) apiPutItems(c *gin.Context, project store.Project) {
	env, ok := s.resolveEnv(c, project)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var input struct {
		Items []itemInput `json:"items"`
	}
	if err := bindJSON(c, &input); err != nil {
		s.fail(c, err)
		return
	}
	keys := make([]string, 0, len(input.Items))
	for _, item := range input.Items {
		if !allowSecretInput(c, item.Secret) {
			return
		}
		keys = append(keys, item.Key)
		if item.Value == nil {
			s.fail(c, fmt.Errorf("%w: key %q requires value", errBadRequest, item.Key))
			return
		}
	}
	if err := validateBatchKeys(keys); err != nil {
		s.fail(c, err)
		return
	}
	identity, _ := auth.FromContext(c.Request.Context())
	items := make([]store.ItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, store.ItemInput{ProjectID: project.ID, EnvironmentID: env.ID, Key: item.Key, Value: *item.Value, Secret: item.Secret, Description: item.Description, UpdatedBy: identity.ActorID()})
	}
	if err := s.store.PutItems(c.Request.Context(), items); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
