package handlers

import (
	"net/http"
	"strings"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// tagNameList joins the names of a tag list comma-separated for
// display texts (notification mails, webhooks) - replacement for the former
// single Country field, which several tags can now replace. An empty list
// (resource with no tag at all) yields an empty string, no placeholder like
// "global".
func tagNameList(tags []models.Tag) string {
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Name
	}
	return strings.Join(names, ", ")
}

// tagIDList extracts the IDs of a tag list - for API responses that only need to
// return the assigned tag IDs to the frontend (e.g. login/me
// response), not the full tag objects.
func tagIDList(tags []models.Tag) []uint {
	ids := make([]uint, len(tags))
	for i, t := range tags {
		ids[i] = t.ID
	}
	return ids
}

// userTagsAndAdmin loads for the logged-in user (from the Gin context) a
// fresh database view of IsGlobalAdmin and the assigned tags
// (step 4/4). Deliberately a fresh DB query instead of
// a JWT claims value (as with the old AllowedCountries) - tags can
// change more often than roles, and a query per request avoids
// a user retaining access after tag revocation until the next login
// via a stale claim.
func (h *Handler) userTagsAndAdmin(c *gin.Context) (tags []models.Tag, isGlobalAdmin bool) {
	userID := getUserID(c)
	if userID == 0 {
		return nil, false
	}
	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		return nil, false
	}
	if middleware.EffectiveGlobalAdmin(c, user) {
		return nil, true
	}
	h.DB.Model(&models.Tag{}).
		Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userID).
		Find(&tags)
	return tags, false
}

// tagsForComponent/tagsForIncident load the currently assigned tags of a
// resource - for the access check on EXISTING components/incidents
// (e.g. when reading/deleting), separate from the TagIDs from a request body
// (on create/update), which still have to be validated.
func (h *Handler) tagsForComponent(componentID uint) []models.Tag {
	return tagsForComponentDB(h.DB, componentID)
}

// tagsForComponentDB reads the tags via db (e.g. a transaction).
func tagsForComponentDB(db *gorm.DB, componentID uint) []models.Tag {
	var tags []models.Tag
	db.Model(&models.Tag{}).
		Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.component_id = ? AND component_tags.deleted_at IS NULL", componentID).
		Find(&tags)
	return tags
}

// tagsByComponentOfPage reads the tags of all components of a page in a
// single query (subquery on the page's components, so that neither a
// list of IDs nor a query per component is needed). Deleted tags,
// assignments and components are absent as with tagsForComponentDB.
func (h *Handler) tagsByComponentOfPage(pageID uint) componentTagMap {
	var rows []struct {
		models.Tag
		OwnerComponentID uint
	}
	h.DB.Model(&models.Tag{}).
		Select("tags.*, component_tags.component_id AS owner_component_id").
		Joins("JOIN component_tags ON component_tags.tag_id = tags.id").
		Where("component_tags.deleted_at IS NULL AND component_tags.component_id IN (?)",
			h.DB.Model(&models.Component{}).Select("id").Where("status_page_id = ?", pageID)).
		Order("tags.id").
		Scan(&rows)
	out := make(componentTagMap, len(rows))
	for _, r := range rows {
		out[r.OwnerComponentID] = append(out[r.OwnerComponentID], r.Tag)
	}
	return out
}

// componentTagMap maps component IDs to their tags.
type componentTagMap map[uint][]models.Tag

// of returns the tags of the component, an empty list without tags (JSON [] instead of null).
func (m componentTagMap) of(componentID uint) []models.Tag {
	if tags := m[componentID]; tags != nil {
		return tags
	}
	return []models.Tag{}
}

func (h *Handler) tagsForIncident(incidentID uint) []models.Tag {
	var tags []models.Tag
	h.DB.Model(&models.Tag{}).
		Joins("JOIN incident_tags ON incident_tags.tag_id = tags.id").
		Where("incident_tags.incident_id = ? AND incident_tags.deleted_at IS NULL", incidentID).
		Find(&tags)
	return tags
}

// tagsForUser loads the tags of a specific user directly by ID, without the
// Gin context - for places outside a request such as
// DispatchNotifications, which for every internal user to be notified
// (not just the logged-in caller) must check whether that user's tags match the
// incident.
func (h *Handler) tagsForUser(userID uint) []models.Tag {
	var tags []models.Tag
	h.DB.Model(&models.Tag{}).
		Joins("JOIN user_tags ON user_tags.tag_id = tags.id").
		Where("user_tags.user_id = ?", userID).
		Find(&tags)
	return tags
}

// userHasTagAccess combines userTagsAndAdmin + middleware.UserHasAccessToAnyTag.
// Tags are an ADDITIONAL, optional restriction on top of the actual
// access control (RequirePermission/UserStatusPageRole, main.go), not a
// replacement for it - whoever already has write permission on a status page by role should
// not be additionally locked out just because neither they nor the
// resource have (yet) been assigned tags. The check therefore only
// actually restricts when BOTH sides (user AND resource)
// carry at least one tag:
//   - Global admins bypass the check entirely (like AllowedCountries="global" before).
//   - A resource with no tag at all is visible/manageable for anyone with page permission.
//   - A user with no tag at all is not restricted by tags (tags are
//     opt-in as a filter, not a mandatory unlock attribute).
//   - Only when both sides have tags does middleware.UserHasAccessToAnyTag
//     decide on a real overlap (incl. the global wildcard tag).
func (h *Handler) userHasTagAccess(c *gin.Context, resourceTags []models.Tag) bool {
	userTags, isGlobalAdmin := h.userTagsAndAdmin(c)
	if isGlobalAdmin || len(resourceTags) == 0 || len(userTags) == 0 {
		return true
	}
	return middleware.UserHasAccessToAnyTag(userTags, resourceTags)
}

// resolveTagsByIDs loads and validates a set of tag IDs from a
// request body (create/update of component/incident): every tag
// must either be global (StatusPageID == nil) or bound to EXACTLY the
// status page for which the resource is created/changed - a
// tag of a FOREIGN status page must not be assigned, otherwise
// an admin with rights on page A could make a resource on page B visible via a
// tag foreign there. Empty tagIDs are allowed (resource with no
// tag at all) - that is not a wildcard but fail-closed, i.e.
// deliberately more restrictive than the old "empty = global" default.
func (h *Handler) resolveTagsByIDs(tagIDs []uint, statusPageID uint) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}
	var tags []models.Tag
	if err := h.DB.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
	}
	for _, t := range tags {
		if t.StatusPageID != nil && *t.StatusPageID != statusPageID {
			// Same response as for an unknown ID and without the tag name:
			// otherwise the message would reveal the existence and names of tags of other
			// tenants (tag IDs are ascending integers).
			return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
		}
	}
	return tags, nil
}

// errGlobalTagForbidden is the error code when a page role tries to
// assign a GLOBAL tag.
const errGlobalTagForbidden = "global_tag_forbidden"

// userMayAssignTags checks whether the caller may assign the tags.
// Global Admins may do everything, page roles only page-owned tags.
// Global tags (e.g. region tags, which also control user access) remain
// reserved for Global Admins. Global tags that the resource already carries
// (current) may be sent along unchanged when editing, otherwise
// a page role could no longer save an entry with a global tag.
// On violation it writes 403 with a code and returns false.
func (h *Handler) userMayAssignTags(c *gin.Context, newTags, current []models.Tag) bool {
	_, isGlobalAdmin := h.userTagsAndAdmin(c)
	if isGlobalAdmin {
		return true
	}
	have := make(map[uint]bool, len(current))
	for _, t := range current {
		have[t.ID] = true
	}
	for _, t := range newTags {
		if t.StatusPageID == nil && !have[t.ID] {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Forbidden: Globale Tags dürfen nur Global Admins vergeben",
				"code":  errGlobalTagForbidden,
			})
			return false
		}
	}
	return true
}

// resolveGlobalTagsByIDs is the user-specific variant of
// resolveTagsByIDs: user tags are - as AllowedCountries
// before - deliberately cross-page, so a user is assigned only
// global tags (StatusPageID == nil), no page-bound ones.
func (h *Handler) resolveGlobalTagsByIDs(tagIDs []uint) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}
	var tags []models.Tag
	if err := h.DB.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newCodedError("tag_not_found", "ein oder mehrere Tags wurden nicht gefunden")
	}
	for _, t := range tags {
		if t.StatusPageID != nil {
			return nil, newCodedError("tag_page_bound", "ein seitengebundener Tag kann keinem User zugewiesen werden")
		}
	}
	return tags, nil
}

// replaceComponentTags/replaceIncidentTags replace the tag assignment of a
// resource completely (delete all + insert new) - the same simple,
// consistent pattern as the UserStatusPageRole reassignment in user.go, instead of
// a diff between the old and new tag set.
func (h *Handler) replaceComponentTags(tx *gorm.DB, componentID uint, tags []models.Tag) error {
	if err := tx.Where("component_id = ?", componentID).Delete(&models.ComponentTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.ComponentTag{ComponentID: componentID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) replaceIncidentTags(tx *gorm.DB, incidentID uint, tags []models.Tag) error {
	if err := tx.Where("incident_id = ?", incidentID).Delete(&models.IncidentTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.IncidentTag{IncidentID: incidentID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) replaceUserTags(tx *gorm.DB, userID uint, tags []models.Tag) error {
	if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&models.UserTag{}).Error; err != nil {
		return err
	}
	for _, t := range tags {
		if err := tx.Create(&models.UserTag{UserID: userID, TagID: t.ID}).Error; err != nil {
			return err
		}
	}
	return nil
}

// incidentTagFilterScope is the tag counterpart to middleware.GeoFilterScope;
// it restricts an incident query to the incidents that the
// logged-in user may see according to tag access. Like userHasTagAccess this is
// an ADDITIONAL, optional restriction on top of the actual
// page access control, not a replacement for it (see the comment there) -
// filtering only happens if the user ACTUALLY carries (non-global) tags.
// Without tags on the user, for Global Admins or with the reserved global
// user tag there is no filter (full view of all incidents of this
// page). An incident with no tag of its own also always remains
// visible, regardless of the user's tags.
func (h *Handler) incidentTagFilterScope(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	userTags, isGlobalAdmin := h.userTagsAndAdmin(c)

	hasGlobalUserTag := false
	var userTagIDs []uint
	for _, t := range userTags {
		if middleware.IsGlobalTag(t) {
			hasGlobalUserTag = true
		}
		userTagIDs = append(userTagIDs, t.ID)
	}

	return func(db *gorm.DB) *gorm.DB {
		if isGlobalAdmin || hasGlobalUserTag || len(userTags) == 0 {
			return db
		}
		return db.Where(
			`NOT EXISTS (
				SELECT 1 FROM incident_tags it0
				WHERE it0.incident_id = incidents.id AND it0.deleted_at IS NULL
			)
			OR EXISTS (
				SELECT 1 FROM incident_tags it
				JOIN tags t ON t.id = it.tag_id
				WHERE it.incident_id = incidents.id
				AND it.deleted_at IS NULL
				AND (it.tag_id IN ? OR (t.status_page_id IS NULL AND LOWER(t.name) = 'global'))
			)`,
			userTagIDs,
		)
	}
}
