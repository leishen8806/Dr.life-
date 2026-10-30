package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
	"github.com/leishen8806/Dr.life-/backend/internal/files"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

// maxBodyBytes 限制请求体大小，避免超大 JSON 进入内存。
const maxBodyBytes = 1 << 20

// MerchantResolver 把网关身份（iam.account.public_id）解析为商户内部 id。
// 跨模块读取由平台侧适配器实现，catalog 模块只消费这个端口（AGENTS.md 规则 8）。
type MerchantResolver interface {
	// MerchantID 返回账号对应的商户 id；账号不是商户、已停用或不存在时 found = false。
	MerchantID(ctx context.Context, accountID string) (merchantID int64, found bool, err error)
}

type TelegramLogin interface {
	Login(context.Context, iam.LoginInput) (iam.LoginResult, error)
}

type SessionManagement interface {
	Refresh(context.Context, string, string) (iam.RefreshResult, error)
	ListSessions(context.Context, string) ([]iam.SessionInfo, error)
	RevokeSession(context.Context, string, string, string) error
}

// AccountProfileManagement exposes only self-service IAM profile operations.
type AccountProfileManagement interface {
	Me(context.Context, string) (iam.AccountProfile, error)
	SetLanguage(context.Context, string, string) error
	Devices(context.Context, string, string) ([]iam.AccountDevice, error)
}

// AccountLinkManagement is the HTTP-facing IAM port for the merchant account-link flow.
// It deliberately exposes candidate public IDs only; matching people and owner authorization stay in IAM.
type AccountLinkManagement interface {
	CandidateBuyers(context.Context, string) ([]iam.AccountLinkCandidate, error)
	Link(context.Context, string, string) (iam.AccountLink, error)
	Unlink(context.Context, string, string) error
}

// AccountDirectory is the only L-09a bridge from ledger to IAM. The ledger
// schema stores owner_account as an opaque IAM id and must not query iam.account
// directly.
type AccountDirectory interface {
	Resolve(context.Context, string) (iam.AccountResolution, bool, error)
}

// AdminPermissionChecker remains fail-closed until A-06b supplies the
// production adapter. Test handlers inject an explicit fake.
type AdminPermissionChecker interface {
	Can(context.Context, string, string, string) (bool, error)
	HasDataScope(context.Context, string) (bool, error)
}

type LedgerReader interface {
	ListAdminTransactions(context.Context, int) ([]ledger.AdminTransaction, error)
	GetAdminTransaction(context.Context, string) (ledger.AdminTransaction, bool, error)
	ListAccountReconciliations(context.Context) ([]ledger.AdminAccountReconciliation, error)
	ListAccountPostings(context.Context, int64, int) ([]ledger.AdminPosting, error)
	ListWalletTransactions(context.Context, ledger.WalletTransactionQuery) (ledger.WalletTransactionPage, error)
	GetWalletTransaction(context.Context, int64, string) (ledger.WalletTransaction, bool, error)
	FinancialCenter(context.Context, ledger.FinancialCenterFilter) (ledger.FinancialCenterReport, error)
}

type FileManagement interface {
	Presign(context.Context, int64, files.PresignInput) (files.PresignResult, error)
	Complete(context.Context, int64, string) (files.File, error)
	DownloadURL(context.Context, int64, string) (storage.SignedURL, error)
}

// Handler 实现 gen.ServerInterface。
// 零值 Handler 只提供 /v1/public/ping；商品接口在依赖未注入时返回 500，
// 这样 server.NewHandler(nil) 与契约测试的既有行为保持不变。
type Handler struct {
	Catalog          *catalog.Service
	Merchants        MerchantResolver
	Logger           *slog.Logger
	Login            TelegramLogin
	Sessions         SessionManagement
	AccountProfiles  AccountProfileManagement
	AccountLinks     AccountLinkManagement
	ClientIP         gateway.ClientIPResolver
	Audit            *platformaudit.Store
	AuditWriter      platformaudit.Writer
	Ledger           LedgerReader
	Directory        AccountDirectory
	AdminPermissions AdminPermissionChecker
	Files            FileManagement
}

func (Handler) GetPublicPing(w http.ResponseWriter, r *http.Request) {
	httpx.WriteOK(w, r, map[string]bool{"pong": true})
}

func (h Handler) GetAdminLedgerTxns(w http.ResponseWriter, r *http.Request) {
	if !h.adminLedgerAccess(w, r) {
		return
	}
	if h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	items, err := h.Ledger.ListAdminTransactions(r.Context(), 30)
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, map[string]any{"items": items})
}

func (h Handler) GetAdminFinancialCenter(w http.ResponseWriter, r *http.Request, params gen.GetAdminFinancialCenterParams) {
	if !h.adminLedgerAccess(w, r) {
		return
	}
	if h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	filter := ledger.FinancialCenterFilter{}
	if params.From != nil {
		filter.From = *params.From
	}
	if params.To != nil {
		filter.To = *params.To
	}
	if params.MerchantLimit != nil {
		filter.MerchantLimit = *params.MerchantLimit
	}
	report, err := h.Ledger.FinancialCenter(r.Context(), filter)
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, report)
}

func (h Handler) GetAdminLedgerTxn(w http.ResponseWriter, r *http.Request, txnNo string) {
	if !h.adminLedgerAccess(w, r) {
		return
	}
	if h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	item, found, err := h.Ledger.GetAdminTransaction(r.Context(), txnNo)
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	if found {
		httpx.WriteOK(w, r, item)
		return
	}
	httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
}

func (h Handler) GetAdminLedgerAccounts(w http.ResponseWriter, r *http.Request) {
	if !h.adminLedgerAccess(w, r) {
		return
	}
	if h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	items, err := h.Ledger.ListAccountReconciliations(r.Context())
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, map[string]any{"items": items})
}

func (h Handler) GetAdminLedgerAccountPostings(w http.ResponseWriter, r *http.Request, accountRef string) {
	if !h.adminLedgerAccess(w, r) {
		return
	}
	if h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	accountID, err := strconv.ParseInt(accountRef, 10, 64)
	if err != nil || accountID <= 0 {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return
	}
	items, err := h.Ledger.ListAccountPostings(r.Context(), accountID, 30)
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	if len(items) == 0 {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
		return
	}
	httpx.WriteOK(w, r, map[string]any{"items": items})
}

func (h Handler) GetBuyerWalletTransactions(w http.ResponseWriter, r *http.Request) {
	h.walletTransactions(w, r, "buyer")
}
func (h Handler) GetMerchantWalletTransactions(w http.ResponseWriter, r *http.Request) {
	h.walletTransactions(w, r, "merchant")
}
func (h Handler) GetDriverWalletTransactions(w http.ResponseWriter, r *http.Request) {
	h.walletTransactions(w, r, "driver")
}
func (h Handler) GetBuyerWalletTransaction(w http.ResponseWriter, r *http.Request, txnNo string) {
	h.walletTransaction(w, r, "buyer", txnNo)
}
func (h Handler) GetMerchantWalletTransaction(w http.ResponseWriter, r *http.Request, txnNo string) {
	h.walletTransaction(w, r, "merchant", txnNo)
}
func (h Handler) GetDriverWalletTransaction(w http.ResponseWriter, r *http.Request, txnNo string) {
	h.walletTransaction(w, r, "driver", txnNo)
}

func (h Handler) adminLedgerAccess(w http.ResponseWriter, r *http.Request) bool {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return false
	}
	if identity.Role != gateway.RoleAdmin || h.AdminPermissions == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return false
	}
	allowed, err := h.AdminPermissions.Can(r.Context(), identity.AccountID, "fund", "view")
	if err != nil || !allowed {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return false
	}
	scoped, err := h.AdminPermissions.HasDataScope(r.Context(), identity.AccountID)
	if err != nil || scoped {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return false
	}
	return true
}

func (h Handler) walletTransactions(w http.ResponseWriter, r *http.Request, end string) {
	owner, ok := h.walletOwner(w, r, end)
	if !ok {
		return
	}
	items, err := h.Ledger.ListWalletTransactions(r.Context(), ledger.WalletTransactionQuery{OwnerAccountID: owner.ID, PageSize: 30})
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, items)
}

func (h Handler) walletTransaction(w http.ResponseWriter, r *http.Request, end, txnNo string) {
	owner, ok := h.walletOwner(w, r, end)
	if !ok {
		return
	}
	item, found, err := h.Ledger.GetWalletTransaction(r.Context(), owner.ID, txnNo)
	if err != nil {
		h.ledgerError(w, r, err)
		return
	}
	if !found {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
		return
	}
	httpx.WriteOK(w, r, item)
}

func (h Handler) walletOwner(w http.ResponseWriter, r *http.Request, end string) (iam.AccountResolution, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return iam.AccountResolution{}, false
	}
	if string(identity.Role) != end || h.Directory == nil || h.Ledger == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return iam.AccountResolution{}, false
	}
	account, found, err := h.Directory.Resolve(r.Context(), identity.AccountID)
	if err != nil {
		h.ledgerError(w, r, err)
		return iam.AccountResolution{}, false
	}
	if !found || account.Status != "active" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
		return iam.AccountResolution{}, false
	}
	if end == "merchant" && account.Role == "merchant_staff" && !identity.HasPermission("fin", "view") {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return iam.AccountResolution{}, false
	}
	return account, true
}

func (h Handler) ledgerError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ledger.ErrInvalidRequest) {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return
	}
	if h.Logger != nil {
		h.Logger.ErrorContext(r.Context(), "ledger read failed", "request_id", httpx.RequestIDFromContext(r.Context()))
	}
	httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
}

func (h Handler) ListAdminAudit(w http.ResponseWriter, r *http.Request, params gen.ListAdminAuditParams) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return
	}
	if identity.Role != gateway.RoleAdmin || !identity.HasPermission("aud", "view") {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return
	}
	if h.Audit == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	query := platformaudit.Query{From: params.From, To: params.To, ActorID: params.ActorId}
	if params.Action != nil {
		query.Action = platformaudit.Action(*params.Action)
	}
	if params.Target != nil {
		query.Target = *params.Target
	}
	if params.BeforeId != nil {
		query.BeforeID = *params.BeforeId
	}
	if params.PageSize != nil {
		query.Limit = *params.PageSize
	}
	page, err := h.Audit.List(r.Context(), query)
	if err != nil {
		if h.Logger != nil {
			h.Logger.ErrorContext(r.Context(), "audit query failed", "request_id", httpx.RequestIDFromContext(r.Context()))
		}
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return
	}
	items := make([]gen.AuditRecord, 0, len(page.Items))
	for _, record := range page.Items {
		var detail map[string]any
		if err := json.Unmarshal(record.Detail, &detail); err != nil {
			httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
			return
		}
		item := gen.AuditRecord{Id: record.ID, Ts: record.Time, ActorId: record.ActorID, ActorRole: record.ActorRole, Action: string(record.Action), Target: record.Target, Detail: detail, RequestId: record.RequestID}
		if record.IP != "" {
			item.Ip = &record.IP
		}
		items = append(items, item)
	}
	httpx.WriteOK(w, r, gen.AuditListData{Items: items, NextBeforeId: page.NextBefore})
}

func (h Handler) PostTelegramAuth(w http.ResponseWriter, r *http.Request) {
	if h.Login == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	var request gen.TelegramAuthRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, telegram.MaxInitDataBytes+1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	if err != nil || trailingErr != io.EOF || request.InitData == "" || len(request.InitData) > telegram.MaxInitDataBytes || !telegram.End(request.End).Valid() {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return
	}
	device := ""
	if request.DeviceId != nil {
		device = *request.DeviceId
	}
	result, err := h.Login.Login(r.Context(), iam.LoginInput{End: telegram.End(request.End), InitData: request.InitData, DeviceID: device, IP: h.ClientIP.Resolve(r)})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, result)
}

func (h Handler) PostAuthRefresh(w http.ResponseWriter, r *http.Request) {
	if h.Sessions == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	body, ok := decodeJSON[gen.RefreshRequest](w, r)
	if !ok || body.RefreshToken == "" {
		if ok {
			httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		}
		return
	}
	result, err := h.Sessions.Refresh(r.Context(), body.RefreshToken, h.ClientIP.Resolve(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, result)
}

func (h Handler) PostBuyerAuthLogout(w http.ResponseWriter, r *http.Request, _ gen.PostBuyerAuthLogoutParams) {
	h.logout(w, r)
}
func (h Handler) PostMerchantAuthLogout(w http.ResponseWriter, r *http.Request, _ gen.PostMerchantAuthLogoutParams) {
	h.logout(w, r)
}
func (h Handler) PostDriverAuthLogout(w http.ResponseWriter, r *http.Request, _ gen.PostDriverAuthLogoutParams) {
	h.logout(w, r)
}

func (h Handler) GetBuyerMeSessions(w http.ResponseWriter, r *http.Request)    { h.listSessions(w, r) }
func (h Handler) GetMerchantMeSessions(w http.ResponseWriter, r *http.Request) { h.listSessions(w, r) }
func (h Handler) GetDriverMeSessions(w http.ResponseWriter, r *http.Request)   { h.listSessions(w, r) }

func (h Handler) GetBuyerMe(w http.ResponseWriter, r *http.Request)    { h.me(w, r) }
func (h Handler) GetMerchantMe(w http.ResponseWriter, r *http.Request) { h.me(w, r) }
func (h Handler) GetDriverMe(w http.ResponseWriter, r *http.Request)   { h.me(w, r) }

func (h Handler) PutBuyerMeLanguage(w http.ResponseWriter, r *http.Request, _ gen.PutBuyerMeLanguageParams) {
	h.setLanguage(w, r)
}
func (h Handler) PutMerchantMeLanguage(w http.ResponseWriter, r *http.Request, _ gen.PutMerchantMeLanguageParams) {
	h.setLanguage(w, r)
}
func (h Handler) PutDriverMeLanguage(w http.ResponseWriter, r *http.Request, _ gen.PutDriverMeLanguageParams) {
	h.setLanguage(w, r)
}

func (h Handler) GetBuyerMeDevices(w http.ResponseWriter, r *http.Request)    { h.devices(w, r) }
func (h Handler) GetMerchantMeDevices(w http.ResponseWriter, r *http.Request) { h.devices(w, r) }
func (h Handler) GetDriverMeDevices(w http.ResponseWriter, r *http.Request)   { h.devices(w, r) }

func (h Handler) DeleteBuyerMeSession(w http.ResponseWriter, r *http.Request, id gen.SessionID, _ gen.DeleteBuyerMeSessionParams) {
	h.deleteSession(w, r, string(id))
}
func (h Handler) DeleteMerchantMeSession(w http.ResponseWriter, r *http.Request, id gen.SessionID, _ gen.DeleteMerchantMeSessionParams) {
	h.deleteSession(w, r, string(id))
}
func (h Handler) DeleteDriverMeSession(w http.ResponseWriter, r *http.Request, id gen.SessionID, _ gen.DeleteDriverMeSessionParams) {
	h.deleteSession(w, r, string(id))
}

func (h Handler) GetMerchantMeLinkCandidates(w http.ResponseWriter, r *http.Request) {
	links, accountID, ok := h.accountLinks(w, r)
	if !ok {
		return
	}
	candidates, err := links.CandidateBuyers(r.Context(), accountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]gen.AccountLinkCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, gen.AccountLinkCandidate{PublicId: candidate.AccountID})
	}
	httpx.WriteOK(w, r, gen.AccountLinkCandidateListData{Items: items})
}

func (h Handler) CreateMerchantMeLink(w http.ResponseWriter, r *http.Request, _ gen.CreateMerchantMeLinkParams) {
	links, accountID, ok := h.accountLinks(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.CreateMerchantMeLinkJSONRequestBody](w, r)
	if !ok || body.BuyerAccountId == "" {
		if ok {
			httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		}
		return
	}
	link, err := links.Link(r.Context(), accountID, body.BuyerAccountId)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, gen.AccountLink{PublicId: link.PublicID, BuyerAccountId: link.BuyerAccountID})
}

func (h Handler) DeleteMerchantMeLink(w http.ResponseWriter, r *http.Request, id gen.AccountLinkID, _ gen.DeleteMerchantMeLinkParams) {
	links, accountID, ok := h.accountLinks(w, r)
	if !ok {
		return
	}
	if err := links.Unlink(r.Context(), accountID, publicIDFromPath(id)); err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, gen.AccountLinkDeletedData{Unlinked: gen.AccountLinkDeletedDataUnlinkedTrue})
}

func (h Handler) logout(w http.ResponseWriter, r *http.Request) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if h.Sessions == nil || !ok || identity.SessionID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return
	}
	if err := h.Sessions.RevokeSession(r.Context(), identity.AccountID, identity.SessionID, "logout"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, map[string]bool{"revoked": true})
}

func (h Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if h.Sessions == nil || !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return
	}
	items, err := h.Sessions.ListSessions(r.Context(), identity.AccountID)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return
	}
	response := make([]gen.Session, 0, len(items))
	for _, item := range items {
		response = append(response, gen.Session{PublicId: item.PublicID, DeviceId: item.DeviceID, IpPrefix: item.IPPrefix, LastUsedAt: item.LastUsed, ExpiresAt: item.Expires})
	}
	httpx.WriteOK(w, r, gen.SessionListData{Items: response})
}

func (h Handler) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if h.Sessions == nil || !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return
	}
	if err := h.Sessions.RevokeSession(r.Context(), identity.AccountID, strings.ToUpper(sessionID), "user_revoked"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, map[string]bool{"revoked": true})
}

func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	profiles, identity, ok := h.accountProfile(w, r)
	if !ok {
		return
	}
	profile, err := profiles.Me(r.Context(), identity.AccountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, profileData(profile))
}

func (h Handler) setLanguage(w http.ResponseWriter, r *http.Request) {
	profiles, identity, ok := h.accountProfile(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.LanguageUpdateRequest](w, r)
	if !ok {
		return
	}
	if err := profiles.SetLanguage(r.Context(), identity.AccountID, string(body.Lang)); err != nil {
		h.writeError(w, r, err)
		return
	}
	profile, err := profiles.Me(r.Context(), identity.AccountID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, profileData(profile))
}

func (h Handler) devices(w http.ResponseWriter, r *http.Request) {
	profiles, identity, ok := h.accountProfile(w, r)
	if !ok {
		return
	}
	items, err := profiles.Devices(r.Context(), identity.AccountID, identity.SessionID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response := make([]gen.AccountDevice, 0, len(items))
	for _, item := range items {
		response = append(response, gen.AccountDevice{PublicId: item.PublicID, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, IsCurrent: item.IsCurrent})
	}
	httpx.WriteOK(w, r, gen.AccountDeviceListData{Items: response})
}

func (h Handler) accountProfile(w http.ResponseWriter, r *http.Request) (AccountProfileManagement, gateway.Identity, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if h.AccountProfiles == nil || !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return nil, gateway.Identity{}, false
	}
	return h.AccountProfiles, identity, true
}

func profileData(profile iam.AccountProfile) gen.AccountProfileData {
	data := gen.AccountProfileData{
		PublicId:      profile.PublicID,
		Role:          gen.AccountProfileDataRole(profile.Role),
		NeedsLanguage: profile.NeedsLanguage,
		Status:        gen.AccountProfileDataStatus(profile.Status),
	}
	if profile.Lang != nil {
		lang := gen.AccountProfileDataLang(*profile.Lang)
		data.Lang = &lang
	}
	return data
}

func (h Handler) PostFilePresign(w http.ResponseWriter, r *http.Request, _ gen.PostFilePresignParams) {
	owner, ok := h.fileOwner(w, r)
	if !ok {
		return
	}
	var body gen.FilePresignRequest
	if !decodeFileJSON(w, r, &body) {
		return
	}
	if h.AuditWriter == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	result, err := h.Files.Presign(r.Context(), owner, files.PresignInput{Purpose: string(body.Purpose), ContentType: string(body.ContentType), Size: body.Size, SHA256: body.Sha256})
	if err != nil {
		h.fileError(w, r, err)
		return
	}
	if !h.appendFileAudit(w, r, owner, platformaudit.ActionFilePresigned, result.File) {
		return
	}
	httpx.WriteOK(w, r, filePresignData(result))
}

func (h Handler) PostFileComplete(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.PostFileCompleteParams) {
	owner, ok := h.fileOwner(w, r)
	if !ok {
		return
	}
	if h.AuditWriter == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	file, err := h.Files.Complete(r.Context(), owner, string(publicID))
	if err != nil {
		h.fileError(w, r, err)
		return
	}
	if !h.appendFileAudit(w, r, owner, platformaudit.ActionFileVerificationRequested, file) {
		return
	}
	httpx.WriteOK(w, r, fileData(file))
}

func (h Handler) GetFileDownloadUrl(w http.ResponseWriter, r *http.Request, publicID gen.PublicID) {
	owner, ok := h.fileOwner(w, r)
	if !ok {
		return
	}
	download, err := h.Files.DownloadURL(r.Context(), owner, string(publicID))
	if err != nil {
		h.fileError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, gen.FileDownloadURLData{DownloadUrl: download.URL, ExpiresAt: download.ExpiresAt})
}

func (h Handler) ListPublicCategories(w http.ResponseWriter, r *http.Request, params gen.ListPublicCategoriesParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	page, err := service.PublicCategories(r.Context(), listQuery(params.Page, params.PageSize, params.Sort, "", ""))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, categoryListData(page))
}

func (h Handler) ListPublicProducts(w http.ResponseWriter, r *http.Request, params gen.ListPublicProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	page, err := service.PublicProducts(r.Context(), listQuery(params.Page, params.PageSize, params.Sort, "", derefString(params.Category)))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, publicProductListData(page))
}

func (h Handler) SearchPublicProducts(w http.ResponseWriter, r *http.Request, params gen.SearchPublicProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	query := catalog.SearchQuery{Query: params.Q, Category: derefString((*string)(params.Category)), Cursor: derefString(params.Cursor)}
	if params.MinPriceE4 != nil {
		query.MinPriceE4 = *params.MinPriceE4
	}
	if params.MaxPriceE4 != nil {
		query.MaxPriceE4 = *params.MaxPriceE4
	}
	if params.Sort != nil {
		query.Sort = catalog.SearchSort(*params.Sort)
	}
	if params.PageSize != nil {
		query.PageSize = int(*params.PageSize)
	}
	page, err := service.PublicSearch(r.Context(), query)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, publicSearchData(page))
}

func (h Handler) SuggestPublicProducts(w http.ResponseWriter, r *http.Request, params gen.SuggestPublicProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	items, err := service.PublicSuggestions(r.Context(), params.Q)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	data := gen.SearchSuggestionData{Items: make([]gen.SearchSuggestion, 0, len(items))}
	for _, item := range items {
		data.Items = append(data.Items, gen.SearchSuggestion{Text: item.Text})
	}
	httpx.WriteOK(w, r, data)
}

func (h Handler) ListMerchantProducts(w http.ResponseWriter, r *http.Request, params gen.ListMerchantProductsParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	var status string
	if params.Status != nil {
		status = string(*params.Status)
	}
	page, err := service.MerchantProducts(r.Context(), merchantID, listQuery(params.Page, params.PageSize, params.Sort, status, ""))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, productListData(page))
}

func (h Handler) GetMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	product, err := service.MerchantProduct(r.Context(), merchantID, publicIDFromPath(publicID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

func (h Handler) CreateMerchantProduct(w http.ResponseWriter, r *http.Request, _ gen.CreateMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.CreateMerchantProductJSONRequestBody](w, r)
	if !ok {
		return
	}
	product, err := service.CreateProduct(r.Context(), merchantID, productInput(body))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	// 创建成功返回 201；envelope 与成功响应 schema 一致。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(gen.ProductSuccessResponse{Ok: true, Data: toGenProduct(product), RequestId: httpx.RequestIDFromContext(r.Context())})
}

func (h Handler) UpdateMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.UpdateMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	body, ok := decodeJSON[gen.UpdateMerchantProductJSONRequestBody](w, r)
	if !ok {
		return
	}
	product, err := service.UpdateProduct(r.Context(), merchantID, publicIDFromPath(publicID), productPatch(body))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

func (h Handler) PublishMerchantProduct(w http.ResponseWriter, r *http.Request, publicID gen.PublicID, _ gen.PublishMerchantProductParams) {
	service, ok := h.catalogService(w, r)
	if !ok {
		return
	}
	merchantID, ok := h.merchantID(w, r)
	if !ok {
		return
	}
	product, err := service.PublishProduct(r.Context(), merchantID, publicIDFromPath(publicID))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, toGenProduct(product))
}

// publicIDFromPath 归一化路径中的 public_id。
// 网关的 unsafePath（internal/platform/gateway/access.go）拒绝含大写字母的路径，
// 而 ULID 的规范形式是大写；Crockford Base32 大小写等价，所以 URL 用小写，
// 这里还原成数据库里的规范大写形式再交给服务层查询。
func publicIDFromPath(publicID string) string {
	return strings.ToUpper(publicID)
}
func (h Handler) catalogService(w http.ResponseWriter, r *http.Request) (*catalog.Service, bool) {
	if h.Catalog == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return nil, false
	}
	return h.Catalog, true
}

func (h Handler) accountLinks(w http.ResponseWriter, r *http.Request) (AccountLinkManagement, string, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return nil, "", false
	}
	if h.AccountLinks == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return nil, "", false
	}
	return h.AccountLinks, identity.AccountID, true
}

// merchantID 解析当前登录商户；身份由网关注入，未登录或角色不符在网关层已拦截。
func (h Handler) merchantID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return 0, false
	}
	if h.Merchants == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return 0, false
	}
	merchantID, found, err := h.Merchants.MerchantID(r.Context(), identity.AccountID)
	if err != nil {
		h.writeError(w, r, err)
		return 0, false
	}
	if !found {
		// 账号不是商户或已停用：统一按越权处理，不泄漏账号是否存在。
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodePermissionDenied))
		return 0, false
	}
	return merchantID, true
}

// writeError maps domain errors to public error codes without exposing internal details.
func (h Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := httpx.CodeInternalError
	var invalid *catalog.ValidationError
	switch {
	case errors.Is(err, iam.ErrAccountLinkNotFound):
		code = httpx.CodeResourceNotFound
	case errors.Is(err, iam.ErrAccountLinkForbidden):
		code = httpx.CodePermissionDenied
	case errors.Is(err, iam.ErrAccountLinkExists):
		code = httpx.CodeAlreadyProcessed
	case errors.Is(err, iam.ErrInvalidLanguage):
		code = httpx.CodeInvalidArgument
	case errors.Is(err, iam.ErrAccountNotFound):
		code = httpx.CodeResourceNotFound
	case errors.Is(err, catalog.ErrNotFound):
		code = httpx.CodeResourceNotFound
	case errors.Is(err, catalog.ErrForbidden), errors.Is(err, catalog.ErrMerchantUnknown):
		code = httpx.CodePermissionDenied
	case errors.As(err, &invalid):
		code = httpx.CodeInvalidArgument
	}
	if code == httpx.CodeInternalError && h.Logger != nil {
		h.Logger.ErrorContext(r.Context(), "api request failed", "error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
	}
	httpx.WriteError(w, r, httpx.NewAppError(code))
}

func (h Handler) fileOwner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok || identity.AccountID == "" {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return 0, false
	}
	if h.Files == nil || h.Directory == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return 0, false
	}
	account, found, err := h.Directory.Resolve(r.Context(), identity.AccountID)
	if err != nil || !found || account.ID <= 0 {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return 0, false
	}
	return account.ID, true
}

func (h Handler) fileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNotReady):
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeResourceNotFound))
	case errors.Is(err, files.ErrQuotaExceeded):
		w.Header().Set("Retry-After", "86400")
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeTooManyRequests))
	case errors.Is(err, files.ErrUnsupportedPurpose), errors.Is(err, storage.ErrChecksumInvalid), errors.Is(err, storage.ErrContentLengthInvalid), errors.Is(err, storage.ErrContentLengthTooLarge), errors.Is(err, storage.ErrContentTypeNotAllowed):
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
	default:
		h.writeError(w, r, err)
	}
}

func (h Handler) appendFileAudit(w http.ResponseWriter, r *http.Request, owner int64, action platformaudit.Action, file files.File) bool {
	identity, ok := gateway.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeUnauthenticated))
		return false
	}
	detail, err := json.Marshal(map[string]any{"purpose": file.Purpose, "content_type": file.ContentType, "size": file.Size, "status": file.Status})
	if err == nil {
		_, err = h.AuditWriter.Append(r.Context(), platformaudit.Entry{ActorID: owner, ActorRole: string(identity.Role), Action: action, Target: "file:" + file.PublicID, Detail: detail, IP: h.ClientIP.Resolve(r), RequestID: httpx.RequestIDFromContext(r.Context())})
	}
	if err != nil {
		if h.Logger != nil {
			h.Logger.ErrorContext(r.Context(), "file audit append failed", "request_id", httpx.RequestIDFromContext(r.Context()))
		}
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInternalError))
		return false
	}
	return true
}

func decodeFileJSON(w http.ResponseWriter, r *http.Request, body *gen.FilePresignRequest) bool {
	bounded := r.Clone(r.Context())
	bounded.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := httpx.DecodeJSON(bounded, body); err != nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return false
	}
	return true
}

func fileData(file files.File) gen.File {
	return gen.File{PublicId: file.PublicID, Purpose: gen.FilePurpose(file.Purpose), ContentType: file.ContentType, Size: file.Size, Status: gen.FileStatus(file.Status), CreatedAt: file.CreatedAt}
}

func filePresignData(result files.PresignResult) gen.FilePresignData {
	file := fileData(result.File)
	return gen.FilePresignData{PublicId: file.PublicId, Purpose: file.Purpose, ContentType: file.ContentType, Size: file.Size, Status: file.Status, CreatedAt: file.CreatedAt, UploadUrl: result.Upload.URL, UploadHeaders: result.Upload.Headers}
}

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var body T
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return body, false
	}
	return body, true
}

func listQuery(page, pageSize *int, sort *string, status, category string) catalog.ListQuery {
	query := catalog.ListQuery{Status: status, Category: category}
	if page != nil {
		query.Page = *page
	}
	if pageSize != nil {
		query.PageSize = *pageSize
	}
	if sort != nil {
		query.Sort = *sort
	}
	return query
}

func productInput(body gen.ProductCreateRequest) catalog.ProductInput {
	input := catalog.ProductInput{
		CategoryCode: body.CategoryCode,
		Name:         fromGenLocalized(body.Name),
		Spec:         fromGenLocalizedPtr(body.Spec),
		Unit:         body.Unit,
		Origin:       string(body.Origin),
		LeadDays:     body.LeadDays,
		VolumeM3:     derefString(body.VolM3),
		Tiers:        fromGenTiers(body.Tiers),
	}
	if body.Source != nil {
		input.Source = string(*body.Source)
	}
	if body.SourceRef != nil {
		input.SourceRef = *body.SourceRef
	}
	return input
}

func productPatch(body gen.ProductUpdateRequest) catalog.ProductPatch {
	patch := catalog.ProductPatch{
		CategoryCode: body.CategoryCode,
		Unit:         body.Unit,
		LeadDays:     body.LeadDays,
		SourceRef:    body.SourceRef,
	}
	if body.Origin != nil {
		origin := string(*body.Origin)
		patch.Origin = &origin
	}
	if body.VolM3 != nil {
		volume := *body.VolM3
		patch.VolumeM3 = &volume
	}
	if body.Status != nil {
		status := catalog.ProductStatus(*body.Status)
		patch.Status = &status
	}
	if body.Name != nil {
		patch.Name = fromGenLocalized(*body.Name)
	}
	if body.Spec != nil {
		patch.Spec = localizedPatch(*body.Spec)
	}
	if body.Tiers != nil {
		patch.Tiers = fromGenTiers(*body.Tiers)
	}
	return patch
}

func productListData(page catalog.ProductPage) gen.ProductListData {
	items := make([]gen.Product, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toGenProduct(item))
	}
	return gen.ProductListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

// publicProductListData 是匿名浏览的投影：供应商来源与内部标准分不属于买家可见信息。
// 商家列表/详情继续使用 productListData，保留这些字段供商家核对。
func publicProductListData(page catalog.ProductPage) gen.ProductListData {
	items := make([]gen.Product, 0, len(page.Items))
	for _, item := range page.Items {
		product := toGenProduct(item)
		product.SourceRef = nil
		product.StdScore = nil
		items = append(items, product)
	}
	return gen.ProductListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

func publicSearchData(page catalog.SearchPage) gen.SearchProductData {
	items := publicSearchProducts(page.Items)
	recommendations := publicSearchProducts(page.Recommendations)
	data := gen.SearchProductData{Items: items, NextCursor: optionalString(page.NextCursor), Recommendations: recommendations}
	if page.RecommendationReason != "" {
		reason := gen.SearchProductDataReason(page.RecommendationReason)
		data.Reason = &reason
	}
	return data
}

func publicSearchProducts(products []catalog.Product) []gen.Product {
	items := make([]gen.Product, 0, len(products))
	for _, item := range products {
		product := toGenProduct(item)
		product.SourceRef, product.StdScore = nil, nil
		items = append(items, product)
	}
	return items
}

func categoryListData(page catalog.CategoryPage) gen.CategoryListData {
	items := make([]gen.Category, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toGenCategory(item))
	}
	return gen.CategoryListData{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total}
}

func toGenCategory(category catalog.Category) gen.Category {
	return gen.Category{
		Code:        category.Code,
		DefaultZone: optionalString(category.DefaultZone),
		Enabled:     category.Enabled,
		Name:        toGenLocalized(category.Name),
		PublicId:    category.PublicID,
		Sort:        category.Sort,
	}
}

func toGenProduct(product catalog.Product) gen.Product {
	tiers := make(gen.ProductTierList, 0, len(product.Tiers))
	for _, tier := range product.Tiers {
		tiers = append(tiers, gen.ProductTier{MinQty: tier.MinQty, UnitPriceE4: tier.UnitPriceE4})
	}
	source := product.Source
	if source == "" {
		source = catalog.SourceManual
	}
	return gen.Product{
		CategoryCode: product.CategoryCode,
		LeadDays:     product.LeadDays,
		Name:         toGenLocalized(product.Name),
		Origin:       gen.ProductOrigin(product.Origin),
		PublicId:     product.PublicID,
		Source:       gen.ProductSource(source),
		SourceRef:    optionalString(product.SourceRef),
		Spec:         toGenLocalizedPtr(product.Spec),
		Status:       gen.ProductStatus(product.Status),
		StdScore:     product.StdScore,
		Tiers:        tiers,
		Unit:         product.Unit,
		UpdatedAt:    product.UpdatedAt,
		VolM3:        optionalString(product.VolumeM3),
	}
}

func toGenLocalized(text catalog.LocalizedText) gen.LocalizedText {
	return gen.LocalizedText{Zh: text[catalog.LangZH], En: text[catalog.LangEN], Km: text[catalog.LangKM]}
}

func toGenLocalizedPtr(text catalog.LocalizedText) *gen.LocalizedText {
	if len(text) == 0 {
		return nil
	}
	localized := toGenLocalized(text)
	return &localized
}

func fromGenLocalized(text gen.LocalizedText) catalog.LocalizedText {
	return catalog.LocalizedText{catalog.LangZH: text.Zh, catalog.LangEN: text.En, catalog.LangKM: text.Km}
}

func fromGenLocalizedPtr(text *gen.LocalizedText) catalog.LocalizedText {
	if text == nil {
		return nil
	}
	return fromGenLocalized(*text)
}

// localizedPatch 映射 PATCH 请求里的三语字段：三个语种全为空白时返回空对象，
// 服务层据此把该列写成 NULL（清空）；任一语种有内容时按原文保留，
// 完整性仍由服务端在发布闸门校验。字段缺席（nil）不走这里，保持"不修改"。
func localizedPatch(text gen.LocalizedText) catalog.LocalizedText {
	localized := fromGenLocalized(text)
	for _, value := range localized {
		if strings.TrimSpace(value) != "" {
			return localized
		}
	}
	return catalog.LocalizedText{}
}

func fromGenTiers(tiers gen.ProductTierList) []catalog.Tier {
	converted := make([]catalog.Tier, 0, len(tiers))
	for _, tier := range tiers {
		converted = append(converted, catalog.Tier{MinQty: tier.MinQty, UnitPriceE4: tier.UnitPriceE4})
	}
	return converted
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
