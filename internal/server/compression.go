package server

import (
	"io/fs"

	"github.com/OXeu/center-common/httpassets"
	"github.com/gin-gonic/gin"
)

func acceptsGzip(value string) bool { return httpassets.AcceptsGzip(value) }

func serveStaticFile(c *gin.Context, filesystem fs.FS, name string) {
	httpassets.ServeFile(c, filesystem, name)
}
