package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/permissions"
)

// RequireAllPermissions requires ALL named permissions, e.g. the
// import of external building blocks, which manages sources and at the same time creates components.
// Someone who has only one of them would otherwise get, via the route, a permission that
// the other resource does not give them.
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

// RequireResolvedPermission requires "<resource>:<action>", where the
// resource is only known at request time: maintenances share
// the routes with incidents, resolve decides by the kind of record
// between "incidents" and "maintenance". Runs after the *PageScope
// middlewares that set :page_id.
func RequireResolvedPermission(action string, resolve func(c *gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		resource := resolve(c)
		if c.IsAborted() { // resolve has answered itself (e.g. 413)
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
