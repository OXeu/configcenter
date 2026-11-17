package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/OXeu/configcenter/internal/store"
)

func maskSecret(value string) string {
	chars := []rune(value)
	if len(chars) <= 8 {
		return "****"
	}
	return string(chars[:3]) + "****" + string(chars[len(chars)-4:])
}

func requireSecretReadPath(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.GetBool("cc_mcp") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "MCP cannot reveal secrets"})
		return
	}
	c.Next()
}

func (s *Server) apiRevealItem(c *gin.Context, project store.Project) {
	c.Set("cc_reveal", true)
	s.apiGetItem(c, project)
}

func (s *Server) apiRevealValues(c *gin.Context) {
	c.Set("cc_reveal", true)
	s.apiProgrammaticItems(c)
}

func (s *Server) apiRevealProposal(c *gin.Context, project store.Project) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		s.fail(c, fmt.Errorf("%w: invalid proposal id", errBadRequest))
		return
	}
	p, err := s.store.ProposalByID(c.Request.Context(), project.ID, uint(id))
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": p.Value})
}
