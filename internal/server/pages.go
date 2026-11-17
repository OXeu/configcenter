package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/auth"
	"github.com/OXeu/configcenter/internal/store"
)

// MountUI registers the server-rendered pages.
func (s *Server) MountUI(r *gin.Engine) {
	ui := r.Group("")
	ui.GET("/projects", auth.RequireUser(), s.projectsPage)
	ui.GET("/projects/:project", auth.RequireUser(), s.projectPage)
	ui.GET("/settings", auth.RequireAdmin(), s.settingsPage)
	ui.GET("/keys", auth.RequireUser(), s.keysPage)
	ui.GET("/users", auth.RequireAdmin(), s.usersPage)
}

func (s *Server) me(c *gin.Context) (store.User, bool) {
	id, ok := auth.FromContext(c.Request.Context())
	if !ok || id.User == nil {
		return store.User{}, false
	}
	return *id.User, true
}

func (s *Server) projectsPage(c *gin.Context) {
	me, ok := s.me(c)
	if !ok {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	s.render(c, http.StatusOK, gin.H{
		"Page":    "projects",
		"Title":   "title.projects",
		"Me":      me,
		"IsAdmin": me.Role == store.RoleAdmin,
	})
}

func (s *Server) projectPage(c *gin.Context) {
	me, ok := s.me(c)
	if !ok {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	project, err := s.store.ProjectBySlug(c.Request.Context(), c.Param("project"))
	if err != nil {
		s.fail(c, err)
		return
	}
	if !s.allowProject(c, project) {
		return
	}
	envs, err := s.store.ListEnvironments(c.Request.Context(), project.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	if len(envs) == 0 {
		s.fail(c, store.ErrNotFound)
		return
	}
	env := envs[0]
	for _, candidate := range envs {
		if candidate.ID == project.PrimaryEnvironmentID {
			env = candidate
			break
		}
	}
	s.render(c, http.StatusOK, gin.H{
		"Page":     "project",
		"Title":    project.Name,
		"Me":       me,
		"IsAdmin":  me.Role == store.RoleAdmin,
		"Project":  &project,
		"Env":      env.Slug,
		"CanWrite": me.Role.CanWrite(),
	})
}

func (s *Server) usersPage(c *gin.Context) {
	me, ok := s.me(c)
	if !ok {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	s.render(c, http.StatusOK, gin.H{
		"Page":    "users",
		"Title":   "title.users",
		"Me":      me,
		"IsAdmin": me.Role == store.RoleAdmin,
	})
}
