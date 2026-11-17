package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/OXeu/configcenter/internal/auth"

	"github.com/gin-gonic/gin"
)

func (s *Server) settingsPage(c *gin.Context) {
	me, ok := s.me(c)
	if !ok {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	s.render(c, http.StatusOK, gin.H{"Page": "settings", "Title": "title.settings", "Me": me, "IsAdmin": true})
}

func (s *Server) apiSettings(c *gin.Context) {
	item, err := s.store.RegistrationSetting(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	var value any = maskSecret(item.Value)
	if !item.Secret {
		allowed, err := strconv.ParseBool(item.Value)
		if err != nil {
			s.fail(c, fmt.Errorf("parse registration policy: %w", strconv.ErrSyntax))
			return
		}
		value = allowed
	}
	c.JSON(http.StatusOK, gin.H{"settings": []gin.H{{"key": item.Key, "value": value, "secret": item.Secret, "masked": item.Secret}}})
}

func (s *Server) apiSaveSettings(c *gin.Context) {
	var body struct {
		Allowed *bool `json:"allow_registration" binding:"required"`
	}
	if err := bindJSON(c, &body); err != nil {
		s.fail(c, err)
		return
	}
	id, _ := auth.FromContext(c.Request.Context())
	if err := s.store.SetRegistrationAllowed(c.Request.Context(), *body.Allowed, id.ActorID()); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
