package handlers

import "github.com/fraigner/statushafen/models"

// IncidentGoogleChatPayloadForTest macht den Google-Chat-Text für die
// Black-Box-Tests (Paket handlers_test) zugänglich.
func IncidentGoogleChatPayloadForTest(inc models.Incident) map[string]interface{} {
	return incidentGoogleChatPayload(notificationDispatchContext{inc: inc, action: "CREATED", compNames: "API", tagNames: "eu"}, "en")
}
