package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// writeRevision answers a read with a strong validator derived from the
// response body. Clients that poll configuration revalidate with
// If-None-Match and receive an empty 304 instead of the full payload.
func (s *Server) writeRevision(c *gin.Context, payload any) {
	if c.GetBool("cc_reveal") {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, payload)
		return
	}
	etag := payloadETag(payload)
	c.Header("ETag", etag)
	c.Header("Cache-Control", "no-cache")
	if matchesETag(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, payload)
}

// payloadETag hashes the encoded response so any value, mask or metadata change
// invalidates cached client revisions.
func payloadETag(payload any) string {
	sum := sha256.Sum256([]byte(jsonString(payload)))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func jsonString(payload any) string {
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return ""
	}
	return builder.String()
}

// matchesETag implements the weak comparison required for If-None-Match.
func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
