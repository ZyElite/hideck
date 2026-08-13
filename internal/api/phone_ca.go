package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) handlePhoneCACertificate(c *gin.Context) {
	path := strings.TrimSpace(s.phoneCACertificate)
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error", "message": "当前使用正式证书，无本地 CA 可下载",
		})
		return
	}
	file, err := os.Open(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "本地 CA 证书不可用"})
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "读取本地 CA 证书失败"})
		return
	}
	c.Header("Content-Type", "application/x-x509-ca-cert")
	c.Header("Content-Disposition", `attachment; filename="vohive-local-ca.crt"`)
	http.ServeContent(c.Writer, c.Request, "vohive-local-ca.crt", stat.ModTime(), file)
}
