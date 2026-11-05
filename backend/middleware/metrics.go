package middleware

import (
	"net/http"
	"time"

	"github.com/fraigner/statushafen/metrics"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// GoldenSignals: zentrale Instrumentierungs-Middleware (Schritt 1/4).
// Erfasst für JEDEN Request Route (normalisiert, siehe unten), HTTP-Methode,
// Status-Code und Latenz und schreibt sie in die übergebene metrics.Registry.
//
// In main.go global einhängen, analog zu CORSMiddleware()/SecurityHeaders():
//
//	r.Use(middleware.GoldenSignals(metrics.Default))
//
// Bewusst eine einzige zentrale Middleware statt Änderungen an einzelnen
// Handlern, damit alle ~45-50 Endpunkte einheitlich erfasst werden.
//
// Route-Normalisierung: c.FullPath() liefert das Gin-Routen-Template (z.B.
// "/api/pages/:page_id/incidents"), NICHT die konkrete URL mit echten IDs –
// das ist entscheidend, um eine Kardinalitätsexplosion in der Aggregation zu
// vermeiden (sonst ein eigener Zähler pro Incident-/Page-ID). Für Requests,
// die auf keine Route matchen (404 via NoRoute), ist c.FullPath() leer; die
// werden bewusst unter der festen Konstante "NOT_FOUND" gebündelt statt unter
// dem rohen Pfad, aus demselben Kardinalitätsgrund.
//
// Performance: Die Middleware hält selbst keinerlei Locks – sie berechnet
// nur die Latenz und delegiert an metrics.Registry.Observe, das pro Route
// ausschließlich atomare Zähler aktualisiert (siehe backend/metrics/metrics.go).
// ─────────────────────────────────────────────────────────────────────────────

// notFoundRouteLabel bündelt alle nicht gematchten Routen, damit ein
// Angreifer nicht durch beliebige Pfade beliebig viele Aggregations-Einträge
// erzeugen kann.
const notFoundRouteLabel = "NOT_FOUND"

// otherMethodLabel bündelt alle HTTP-Methoden außerhalb von knownMethods. Die
// Methode ist ein frei wählbarer Token; als Label würde jede erfundene
// Methode (auch bis rund 1 MB lang) eine neue Zeitreihe anlegen.
const otherMethodLabel = "OTHER"

// knownMethods sind die Methoden, die die API verwendet oder sinnvoll
// beantwortet.
var knownMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodPost: {}, http.MethodPut: {}, http.MethodPatch: {},
	http.MethodDelete: {}, http.MethodHead: {}, http.MethodOptions: {},
}

func methodLabel(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return otherMethodLabel
}

// GoldenSignals gibt eine Gin-Middleware zurück, die jeden Request in reg
// aggregiert. reg ist typischerweise metrics.Default; Tests können eine
// isolierte metrics.NewRegistry() übergeben, um nicht mit anderen Tests zu
// interferieren.
func GoldenSignals(reg *metrics.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)

		route := c.FullPath()
		if route == "" {
			route = notFoundRouteLabel
		}

		reg.Observe(methodLabel(c.Request.Method), route, c.Writer.Status(), latency)
	}
}
