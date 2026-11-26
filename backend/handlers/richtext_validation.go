package handlers

import (
	"net/http"
	"unicode/utf8"

	"github.com/fraigner/statushafen/richtext"

	"github.com/gin-gonic/gin"
)

// requireMessageLength checks the length of a formatted message or
// description (at most richtext.MaxLen characters) and answers
// an overlong input with 400 and the code message_too_long (text de/en under
// api.errors.message_too_long). Return value false = response has already been sent.
// Characters of the Markdown source are counted as they are stored.
func requireMessageLength(c *gin.Context, message string) bool {
	if !richtext.TooLong(message) {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("message_too_long", "Die Nachricht ist zu lang (höchstens 5000 Zeichen)"))
	return false
}

// maxTitleLen is the upper limit (in characters) for titles and page names. A
// row in the admin lists and in the subject of mails thus stays readable; without a
// limit, titles were stored up to the body limit. Component names are
// more tightly constrained by maxComponentNameLen.
const maxTitleLen = 200

// requireTitleLength checks the length of a title in characters (not bytes) and
// answers an overlong input with 400 and the code title_too_long
// (text de/en under api.errors.title_too_long). Return value false = response is
// already sent. An empty title passes the check; required fields are checked by
// the caller.
func requireTitleLength(c *gin.Context, title string) bool {
	if utf8.RuneCountInString(title) <= maxTitleLen {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("title_too_long", "Der Titel ist zu lang (höchstens 200 Zeichen)"))
	return false
}

// requirePageNameLength is requireTitleLength for page names (code
// page_name_too_long).
func requirePageNameLength(c *gin.Context, name string) bool {
	if utf8.RuneCountInString(name) <= maxTitleLen {
		return true
	}
	respondCoded(c, http.StatusBadRequest, newCodedError("page_name_too_long", "Der Seitenname ist zu lang (höchstens 200 Zeichen)"))
	return false
}
