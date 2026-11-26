package handlers

import "github.com/fraigner/statushafen/models"

// IncidentGoogleChatPayloadForTest makes the Google Chat text accessible to the
// black-box tests (package handlers_test).
func IncidentGoogleChatPayloadForTest(inc models.Incident) map[string]interface{} {
	return incidentGoogleChatPayload(notificationDispatchContext{inc: inc, action: "CREATED", compNames: "API", tagNames: "eu"}, "en")
}
