package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/slug"
	"github.com/OXeu/configcenter/internal/store"
)

func (s *Server) apiRenameEnvironment(c *gin.Context, project store.Project) {
	env, ok := s.resolveEnv(c, project)
	if !ok {
		return
	}
	var in envJSON
	if err := bindJSON(c, &in); err != nil {
		s.fail(c, err)
		return
	}
	if err := slug.Validate(in.Slug); err != nil {
		s.fail(c, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		s.fail(c, fmt.Errorf("%w: name is required", errBadRequest))
		return
	}
	if err := s.store.RenameEnvironment(c.Request.Context(), project.ID, env.ID, in.Slug, in.Name); err != nil {
		s.fail(c, err)
		return
	}
	env.Slug, env.Name = in.Slug, in.Name
	c.JSON(http.StatusOK, toEnvJSON(env, project.PrimaryEnvironmentID))
}

func (s *Server) apiSetPrimaryEnvironment(c *gin.Context, project store.Project) {
	env, ok := s.resolveEnv(c, project)
	if !ok {
		return
	}
	if err := s.store.SetPrimaryEnvironment(c.Request.Context(), project.ID, env.ID); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toEnvJSON(env, env.ID))
}
