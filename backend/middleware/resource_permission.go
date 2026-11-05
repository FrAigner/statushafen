package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/permissions"
)

// RequireAllPermissions verlangt ALLE genannten Rechte, z. B. der
// Import externer Bausteine, der Quellen verwaltet und zugleich Komponenten
// anlegt. Wer nur eines davon hat, bekäme sonst über die Route ein Recht, das
// ihm die andere Ressource nicht gibt.
func RequireAllPermissions(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, perm := range required {
			if status, msg, ok := requirePermissionCheck(c, perm); !ok {
				c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// RequireResolvedPermission verlangt "<ressource>:<action>", wobei die
// Ressource erst zur Anfragezeit feststeht: Wartungen teilen sich
// die Routen mit Vorfällen, resolve entscheidet nach der Art des Datensatzes
// zwischen "incidents" und "maintenance". Läuft nach den *PageScope-
// Middlewares, die :page_id setzen.
func RequireResolvedPermission(action string, resolve func(c *gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		resource := resolve(c)
		if c.IsAborted() { // resolve hat selbst geantwortet (z. B. 413)
			return
		}
		if status, msg, ok := requirePermissionCheck(c, permissions.Perm(resource, action)); !ok {
			c.JSON(status, gin.H{"error": msg, "code": codeForStatus(status)})
			c.Abort()
			return
		}
		c.Next()
	}
}
