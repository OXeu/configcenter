package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// loginError maps the callback error code to a translated message.
func (s *Server) loginError(c *gin.Context) string {
	switch code := c.Query("error"); code {
	case "":
		return ""
	case "registration_closed", "oauth_disabled", "oauth_unavailable", "denied", "missing_state", "oauth_failed", "no_account", "session_failed":
		return s.translator(c).T("login.error." + code)
	default:
		return s.translator(c).T("login.error.generic")
	}
}

func (s *Server) redirectWithError(c *gin.Context, code string) {
	c.Redirect(http.StatusFound, "/login?error="+code)
}
