package api

import (
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/notifications"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

func (h Handler) ListBuyerMeNotifications(w http.ResponseWriter, r *http.Request, params gen.ListBuyerMeNotificationsParams) {
	h.listNotifications(w, r, params.Limit)
}

func (h Handler) ListMerchantMeNotifications(w http.ResponseWriter, r *http.Request, params gen.ListMerchantMeNotificationsParams) {
	h.listNotifications(w, r, params.Limit)
}

func (h Handler) listNotifications(w http.ResponseWriter, r *http.Request, limit *int) {
	accountID, ok := h.notificationAccount(w, r)
	if !ok {
		return
	}
	n := 20
	if limit != nil {
		n = *limit
	}
	items, unread, err := h.Notifications.List(r.Context(), accountID, n)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	data := gen.NotificationListData{Items: make([]gen.Notification, 0, len(items)), UnreadCount: int32(unread)}
	for _, item := range items {
		data.Items = append(data.Items, gen.Notification{
			PublicId: item.PublicID, EventId: item.EventID, EventType: string(item.EventType),
			Title: messages(item.Title), Body: messages(item.Body), ReadAt: item.ReadAt, CreatedAt: item.CreatedAt,
		})
	}
	httpx.WriteOK(w, r, data)
}

func (h Handler) MarkBuyerMeNotificationRead(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.MarkBuyerMeNotificationReadParams) {
	h.markNotificationRead(w, r, publicIDFromPath(string(publicID)))
}

func (h Handler) MarkMerchantMeNotificationRead(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.MarkMerchantMeNotificationReadParams) {
	h.markNotificationRead(w, r, publicIDFromPath(string(publicID)))
}

func (h Handler) markNotificationRead(w http.ResponseWriter, r *http.Request, publicID string) {
	accountID, ok := h.notificationAccount(w, r)
	if !ok {
		return
	}
	if err := h.Notifications.MarkRead(r.Context(), accountID, publicID); err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, gen.NotificationMutationData{Updated: gen.NotificationMutationDataUpdatedTrue})
}

func (h Handler) MarkBuyerMeNotificationsReadAll(w http.ResponseWriter, r *http.Request, _ gen.MarkBuyerMeNotificationsReadAllParams) {
	h.markAllNotificationsRead(w, r)
}

func (h Handler) MarkMerchantMeNotificationsReadAll(w http.ResponseWriter, r *http.Request, _ gen.MarkMerchantMeNotificationsReadAllParams) {
	h.markAllNotificationsRead(w, r)
}

func (h Handler) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.notificationAccount(w, r)
	if !ok {
		return
	}
	if err := h.Notifications.MarkAllRead(r.Context(), accountID); err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, gen.NotificationMutationData{Updated: gen.NotificationMutationDataUpdatedTrue})
}

func (h Handler) ListBuyerMeNotificationSettings(w http.ResponseWriter, r *http.Request) {
	h.listNotificationSettings(w, r)
}

func (h Handler) ListMerchantMeNotificationSettings(w http.ResponseWriter, r *http.Request) {
	h.listNotificationSettings(w, r)
}

func (h Handler) listNotificationSettings(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.notificationAccount(w, r)
	if !ok {
		return
	}
	items, err := h.Notifications.ListSettings(r.Context(), accountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, notificationSettingsData(items))
}

func (h Handler) PutBuyerMeNotificationSetting(w http.ResponseWriter, r *http.Request, _ gen.PutBuyerMeNotificationSettingParams) {
	h.putNotificationSetting(w, r)
}

func (h Handler) PutMerchantMeNotificationSetting(w http.ResponseWriter, r *http.Request, _ gen.PutMerchantMeNotificationSettingParams) {
	h.putNotificationSetting(w, r)
}

func (h Handler) putNotificationSetting(w http.ResponseWriter, r *http.Request) {
	accountID, ok := h.notificationAccount(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.NotificationSettingUpdateRequest](w, r)
	if !ok {
		return
	}
	setting := notifications.Setting{EventType: notifications.EventType(body.EventType), InAppEnabled: body.InAppEnabled, TelegramEnabled: body.TelegramEnabled}
	if err := h.Notifications.UpsertSetting(r.Context(), accountID, setting); err != nil {
		h.writeError(w, r, err)
		return
	}
	items, err := h.Notifications.ListSettings(r.Context(), accountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, notificationSettingsData(items))
}

func (h Handler) notificationAccount(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return 0, false
	}
	if h.Notifications == nil || h.Directory == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return 0, false
	}
	account, found, err := h.Directory.Resolve(r.Context(), identity.AccountID)
	if err != nil {
		h.writeError(w, r, err)
		return 0, false
	}
	if !found || account.ID <= 0 {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return 0, false
	}
	return account.ID, true
}

func messages(value map[string]string) gen.Messages {
	return gen.Messages{Zh: value["zh"], En: value["en"], Km: value["km"]}
}

func notificationSettingsData(items []notifications.Setting) gen.NotificationSettingsData {
	data := gen.NotificationSettingsData{Items: make([]gen.NotificationSetting, 0, len(items))}
	for _, item := range items {
		data.Items = append(data.Items, gen.NotificationSetting{EventType: string(item.EventType), InAppEnabled: item.InAppEnabled, TelegramEnabled: item.TelegramEnabled})
	}
	return data
}
