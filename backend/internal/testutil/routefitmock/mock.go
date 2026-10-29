// Package routefitmock provides an in-memory RouteFit platform substitute for
// integration tests. It is deliberately test-only and never contacts RouteFit.
package routefitmock

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	CallbackSecret string
	QuoteTTL       time.Duration
}
type Money struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}
type Owner struct {
	OwnerRef     string   `json:"ownerRef"`
	CustomerID   string   `json:"customerId"`
	WarehouseIDs []string `json:"warehouseIds"`
	OutboundHold string   `json:"outboundHold"`
	Version      int64    `json:"version"`
}
type SKU struct {
	ID              string `json:"skuId"`
	MerchantSKUCode string `json:"merchantSkuCode"`
	Name            string `json:"name"`
	Unit            string `json:"unit"`
	State           string `json:"state"`
	Length          int64  `json:"length"`
	Width           int64  `json:"width"`
	Height          int64  `json:"height"`
	Weight          int64  `json:"weight"`
}
type Evidence struct {
	ID         string    `json:"evidenceId"`
	Kind       string    `json:"kind"`
	SHA256     string    `json:"sha256"`
	CapturedAt time.Time `json:"capturedAt"`
}
type FeeLine struct {
	ID            string `json:"feeLineId"`
	Version       int64  `json:"version"`
	OwnerRef      string `json:"ownerRef"`
	FeeCode       string `json:"feeCode"`
	Direction     string `json:"direction"`
	Amount        Money  `json:"amount"`
	ServiceDate   string `json:"serviceDate"`
	FulfillmentID string `json:"fulfillmentId,omitempty"`
	SKU           string `json:"skuId,omitempty"`
	Quantity      int64  `json:"quantity,omitempty"`
	RateRef       string `json:"rateRef,omitempty"`
}
type Aggregate struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
}
type Event struct {
	ID            string          `json:"eventId"`
	Type          string          `json:"eventType"`
	SchemaVersion string          `json:"schemaVersion"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Aggregate     Aggregate       `json:"aggregate"`
	CorrelationID string          `json:"correlationId,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}
type DeliveryOptions struct {
	Duplicate        bool
	Reverse          bool
	Delay            time.Duration
	InvalidSignature bool
	ExpiredTimestamp bool
	Retry            bool
	Timeout          time.Duration
}
type Fault struct {
	Status int
	Count  int
	Delay  time.Duration
}
type apiError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	Details        any    `json:"details,omitempty"`
	Retryable      bool   `json:"retryable"`
	RequestID      string `json:"requestId"`
	CurrentVersion int64  `json:"currentVersion,omitempty"`
}
type line struct {
	Ref      string `json:"lineRef"`
	SKU      string `json:"skuId"`
	Quantity int64  `json:"quantity"`
}
type fulfillmentRequest struct {
	OwnerRef         string          `json:"ownerRef"`
	ExternalOrderID  string          `json:"externalOrderId"`
	ExternalRevision int64           `json:"externalRevision"`
	WarehouseID      string          `json:"warehouseId"`
	Lines            []line          `json:"lines"`
	Delivery         json.RawMessage `json:"delivery"`
	RequestedWindow  json.RawMessage `json:"requestedDeliveryWindow"`
	ServiceLevel     string          `json:"serviceLevel"`
	PaymentTerms     string          `json:"paymentTerms"`
	DeclaredValue    Money           `json:"declaredValue"`
	SourcePlatform   string          `json:"sourcePlatform"`
	QuoteID          string          `json:"quoteId,omitempty"`
	QuoteVersion     int64           `json:"quoteVersion,omitempty"`
}
type fulfillment struct {
	ID             string
	Request        fulfillmentRequest
	Version        int64
	State          string
	StartedPicking bool
	Evidence       map[string]Evidence
}
type stock struct {
	Quantity int64
	Version  int64
}
type quote struct {
	ID                    string
	Version               int64
	OwnerRef, WarehouseID string
	Lines                 []line
	Total                 Money
	ExpiresAt             time.Time
}

// Mock implements the contract's platform endpoints and test scenario helpers.
type Mock struct {
	mu                sync.Mutex
	config            Config
	owners            map[string]*Owner
	skus              map[string]map[string]SKU
	stocks            map[string]stock
	fulfillments      map[string]*fulfillment
	byExternal        map[string]string
	idempotency       map[string]string
	idempotencyBody   map[string]string
	cancelIdempotency map[string]map[string]any
	events            []Event
	quotes            map[string]quote
	feeLines          map[string]FeeLine
	fault             Fault
	next              int64
}

func New(c Config) *Mock {
	if c.QuoteTTL <= 0 {
		c.QuoteTTL = 30 * time.Minute
	}
	return &Mock{config: c, owners: map[string]*Owner{}, skus: map[string]map[string]SKU{}, stocks: map[string]stock{}, fulfillments: map[string]*fulfillment{}, byExternal: map[string]string{}, idempotency: map[string]string{}, idempotencyBody: map[string]string{}, cancelIdempotency: map[string]map[string]any{}, quotes: map[string]quote{}, feeLines: map[string]FeeLine{}}
}
func (m *Mock) AddOwner(owner Owner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner.OutboundHold == "" {
		owner.OutboundHold = "released"
	}
	if owner.Version == 0 {
		owner.Version = 1
	}
	copy := owner
	m.owners[owner.OwnerRef] = &copy
	if m.skus[owner.OwnerRef] == nil {
		m.skus[owner.OwnerRef] = map[string]SKU{}
	}
}
func (m *Mock) AddSKU(ownerRef string, sku SKU, warehouseID string, qty int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sku.State == "" {
		sku.State = "active"
	}
	if m.skus[ownerRef] == nil {
		m.skus[ownerRef] = map[string]SKU{}
	}
	m.skus[ownerRef][sku.ID] = sku
	m.stocks[stockKey(ownerRef, warehouseID, sku.ID)] = stock{Quantity: qty, Version: 1}
}
func (m *Mock) AddEvidence(id string, e Evidence) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f := m.fulfillments[id]; f != nil {
		if f.Evidence == nil {
			f.Evidence = map[string]Evidence{}
		}
		f.Evidence[e.ID] = e
	}
}
func (m *Mock) AddFeeLine(f FeeLine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.feeLines[f.ID]
	if !ok || f.Version >= old.Version {
		m.feeLines[f.ID] = f
	}
}
func (m *Mock) SetFault(f Fault)      { m.mu.Lock(); defer m.mu.Unlock(); m.fault = f }
func (m *Mock) Handler() http.Handler { return http.HandlerFunc(m.serveHTTP) }
func (m *Mock) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Event(nil), m.events...)
}

// AdvanceFulfillment creates a contract event for a scripted fulfillment state.
func (m *Mock) AdvanceFulfillment(id, state string) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fulfillments[id]
	if f == nil {
		return Event{}, fmt.Errorf("fulfillment %q not found", id)
	}
	eventType, ok := map[string]string{"accepted": "fulfillment.accepted", "picked": "fulfillment.picked", "packed": "fulfillment.packed", "shipped": "fulfillment.shipped", "delivery_reported": "fulfillment.delivery_reported", "delivered_verified": "fulfillment.delivered_verified", "delivery_failed": "fulfillment.delivery_failed", "returned": "fulfillment.returned"}[state]
	if !ok {
		return Event{}, fmt.Errorf("unsupported fulfillment state %q", state)
	}
	f.State = state
	f.Version++
	if state == "picked" || state == "packed" {
		f.StartedPicking = true
	}
	payload, _ := json.Marshal(map[string]any{"fulfillmentId": f.ID, "externalOrderId": f.Request.ExternalOrderID, "fulfillmentState": state, "evidenceSummaries": evidenceList(f.Evidence)})
	return Event{ID: m.id("event"), Type: eventType, SchemaVersion: "v1", OccurredAt: time.Now().UTC(), Aggregate: Aggregate{Type: "fulfillment", ID: f.ID, Version: f.Version}, Payload: payload}, nil
}

func (m *Mock) serveHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	fault := m.fault
	if fault.Count > 0 {
		m.fault.Count--
		m.mu.Unlock()
		if fault.Delay > 0 {
			time.Sleep(fault.Delay)
		}
		status := fault.Status
		if status == 0 {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "UPSTREAM_FAULT", "scripted fault", nil)
		return
	}
	m.mu.Unlock()
	p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/platform/v1/"), "/")
	parts := strings.Split(p, "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "owners":
		m.owner(w, parts[1])
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "owners" && parts[2] == "skus":
		m.listSKUs(w, parts[1])
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "owners" && parts[2] == "inventory":
		m.inventory(w, r, parts[1])
	case r.Method == http.MethodPut && len(parts) == 3 && parts[0] == "owners" && parts[2] == "outbound-hold":
		m.hold(w, r, parts[1])
	case r.Method == http.MethodPost && p == "quotes":
		m.createQuote(w, r)
	case r.Method == http.MethodPost && p == "fulfillments":
		m.createFulfillment(w, r)
	case r.Method == http.MethodGet && p == "fulfillments":
		m.getFulfillment(w, r, "")
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "fulfillments":
		m.getFulfillment(w, r, parts[1])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "fulfillments" && parts[2] == "actions" && parts[3] == "cancel":
		m.cancel(w, r, parts[1])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "fulfillments" && parts[2] == "evidence" && parts[4] == "download-url":
		m.evidenceURL(w, parts[1], parts[3])
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "statements":
		m.statement(w, parts[1])
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "route not found", nil)
	}
}

func (m *Mock) owner(w http.ResponseWriter, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner := m.owners[ref]
	if owner == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"ownerRef": owner.OwnerRef, "customerId": owner.CustomerID, "state": "active", "warehouses": owner.WarehouseIDs, "outboundHold": owner.OutboundHold})
}
func (m *Mock) listSKUs(w http.ResponseWriter, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owners[ref] == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	values := make([]SKU, 0, len(m.skus[ref]))
	for _, s := range m.skus[ref] {
		values = append(values, s)
	}
	writeJSON(w, 200, map[string]any{"items": values, "nextCursor": ""})
}
func (m *Mock) inventory(w http.ResponseWriter, r *http.Request, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owners[ref] == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	want := map[string]bool{}
	for _, id := range strings.Split(r.URL.Query().Get("skuIds"), ",") {
		if id != "" {
			want[id] = true
		}
	}
	var items []map[string]any
	for key, s := range m.stocks {
		bits := strings.Split(key, "|")
		if bits[0] != ref || (len(want) > 0 && !want[bits[2]]) {
			continue
		}
		items = append(items, map[string]any{"ownerRef": ref, "skuId": bits[2], "warehouseId": bits[1], "requestableQty": s.Quantity, "sourceVersion": s.Version})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (m *Mock) hold(w http.ResponseWriter, r *http.Request, ref string) {
	var body struct{ State, ReasonCode, Note string }
	if err := decode(r, &body); err != nil || (body.State != "held" && body.State != "released") {
		writeError(w, 400, "INVALID_REQUEST", "state must be held or released", nil)
		return
	}
	m.mu.Lock()
	owner := m.owners[ref]
	if owner != nil {
		if r.Header.Get("If-Match") != strconv.FormatInt(owner.Version, 10) {
			m.mu.Unlock()
			writeError(w, 412, "VERSION_CONFLICT", "If-Match does not match", map[string]any{"currentVersion": owner.Version})
			return
		}
		owner.OutboundHold = body.State
		owner.Version++
		payload, _ := json.Marshal(map[string]any{"ownerRef": ref, "state": body.State, "reasonCode": body.ReasonCode})
		m.events = append(m.events, Event{ID: m.id("event"), Type: "owner.outbound_hold_changed", SchemaVersion: "v1", OccurredAt: time.Now().UTC(), Aggregate: Aggregate{Type: "owner", ID: ref, Version: owner.Version}, Payload: payload})
	}
	m.mu.Unlock()
	if owner == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"ownerRef": ref, "state": body.State, "reasonCode": body.ReasonCode})
}
func (m *Mock) createQuote(w http.ResponseWriter, r *http.Request) {
	var q struct {
		OwnerRef, WarehouseID, ServiceLevel string
		Lines                               []line
		Delivery                            json.RawMessage
	}
	if err := decode(r, &q); err != nil || q.OwnerRef == "" || q.WarehouseID == "" || len(q.Lines) == 0 {
		writeError(w, 400, "INVALID_REQUEST", "invalid quote", nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owners[q.OwnerRef] == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	var total int64
	for _, l := range q.Lines {
		if l.Quantity <= 0 {
			writeError(w, 400, "INVALID_REQUEST", "quantity must be positive", nil)
			return
		}
		total += l.Quantity * 100
	}
	id := m.id("quote")
	v := quote{ID: id, Version: 1, OwnerRef: q.OwnerRef, WarehouseID: q.WarehouseID, Lines: q.Lines, Total: Money{AmountMinor: total, Currency: "USD"}, ExpiresAt: time.Now().UTC().Add(m.config.QuoteTTL)}
	m.quotes[id] = v
	writeJSON(w, 200, map[string]any{"quoteId": id, "version": v.Version, "fees": []any{map[string]any{"feeCode": "base", "amount": v.Total}}, "total": v.Total, "expiresAt": v.ExpiresAt})
}
func (m *Mock) createFulfillment(w http.ResponseWriter, r *http.Request) {
	var req fulfillmentRequest
	if err := decode(r, &req); err != nil {
		writeError(w, 400, "INVALID_REQUEST", "invalid fulfillment", nil)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required", nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bodyHash, _ := json.Marshal(req)
	if len(key) < 16 || len(key) > 128 {
		writeError(w, 400, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 16 to 128 characters", nil)
		return
	}
	if id := m.idempotency[key]; id != "" {
		if m.idempotencyBody[key] != string(bodyHash) {
			writeError(w, 409, "IDEMPOTENCY_KEY_CONFLICT", "key already used with different content", nil)
			return
		}
		f := m.fulfillments[id]
		writeFulfillment(w, f, true)
		return
	}
	if req.OwnerRef == "" || req.ExternalOrderID == "" || req.ExternalRevision < 1 || req.WarehouseID == "" || len(req.Lines) < 1 || len(req.Lines) > 200 || len(req.Delivery) == 0 || req.ServiceLevel == "" || req.PaymentTerms != "PLATFORM_PREPAID" || req.SourcePlatform != "drlife" || req.DeclaredValue.Currency == "" {
		writeError(w, 400, "INVALID_REQUEST", "required fields are missing or invalid", nil)
		return
	}
	owner := m.owners[req.OwnerRef]
	if owner == nil {
		writeError(w, 404, "OWNER_NOT_FOUND", "owner not found", nil)
		return
	}
	if owner.OutboundHold == "held" {
		writeError(w, 423, "OWNER_OUTBOUND_HELD", "owner outbound is held", nil)
		return
	}
	if oldID := m.byExternal[req.ExternalOrderID]; oldID != "" {
		old := m.fulfillments[oldID]
		oldHash, _ := json.Marshal(old.Request)
		if req.ExternalRevision == old.Request.ExternalRevision && string(oldHash) == string(bodyHash) {
			m.idempotency[key] = old.ID
			m.idempotencyBody[key] = string(bodyHash)
			writeFulfillment(w, old, true)
			return
		}
		if req.ExternalRevision <= old.Request.ExternalRevision {
			writeError(w, 409, "EXTERNAL_REVISION_CONFLICT", "revision must increase", nil)
			return
		}
		if old.StartedPicking {
			writeError(w, 409, "EXTERNAL_REVISION_LOCKED", "picking has started", nil)
			return
		}
	}
	if req.QuoteID != "" {
		q, ok := m.quotes[req.QuoteID]
		if !ok || q.Version != req.QuoteVersion || time.Now().After(q.ExpiresAt) || q.OwnerRef != req.OwnerRef || q.WarehouseID != req.WarehouseID || !sameLines(q.Lines, req.Lines) {
			writeError(w, 409, "QUOTE_VERSION_CONFLICT", "quote is invalid", nil)
			return
		}
	}
	for _, l := range req.Lines {
		sku, ok := m.skus[req.OwnerRef][l.SKU]
		s := m.stocks[stockKey(req.OwnerRef, req.WarehouseID, l.SKU)]
		if !ok || sku.State != "active" {
			writeError(w, 409, "SKU_UNAVAILABLE", "SKU is unavailable", map[string]any{"lines": []line{l}})
			return
		}
		if l.Quantity <= 0 || s.Quantity < l.Quantity {
			writeError(w, 409, "INSUFFICIENT_STOCK", "insufficient stock", map[string]any{"lines": []line{l}})
			return
		}
	}
	for _, l := range req.Lines {
		k := stockKey(req.OwnerRef, req.WarehouseID, l.SKU)
		s := m.stocks[k]
		s.Quantity -= l.Quantity
		s.Version++
		m.stocks[k] = s
	}
	f := &fulfillment{ID: m.id("fulfillment"), Request: req, Version: 1, State: "accepted", Evidence: map[string]Evidence{}}
	m.fulfillments[f.ID] = f
	m.byExternal[req.ExternalOrderID] = f.ID
	m.idempotency[key] = f.ID
	m.idempotencyBody[key] = string(bodyHash)
	writeFulfillment(w, f, false)
}
func (m *Mock) getFulfillment(w http.ResponseWriter, r *http.Request, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if external := r.URL.Query().Get("externalOrderId"); external != "" {
		id = m.byExternal[external]
	}
	f := m.fulfillments[id]
	if f == nil {
		writeError(w, 404, "FULFILLMENT_NOT_FOUND", "fulfillment not found", nil)
		return
	}
	writeJSON(w, 200, fulfillmentResponse(f, false))
}
func (m *Mock) cancel(w http.ResponseWriter, r *http.Request, id string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required", nil)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fulfillments[id]
	if f == nil {
		writeError(w, 404, "FULFILLMENT_NOT_FOUND", "fulfillment not found", nil)
		return
	}
	if r.Header.Get("If-Match") != strconv.FormatInt(f.Version, 10) {
		writeError(w, 412, "VERSION_CONFLICT", "If-Match does not match", map[string]any{"currentVersion": f.Version})
		return
	}
	if cached := m.cancelIdempotency[key]; cached != nil {
		writeJSON(w, 200, cached)
		return
	}
	if f.State == "shipped" {
		writeError(w, 409, "INVALID_TRANSITION", "cannot cancel shipped fulfillment", nil)
		return
	}
	if f.StartedPicking {
		f.State = "cancel_requested"
	} else {
		f.State = "cancelled"
		for _, l := range f.Request.Lines {
			k := stockKey(f.Request.OwnerRef, f.Request.WarehouseID, l.SKU)
			s := m.stocks[k]
			s.Quantity += l.Quantity
			s.Version++
			m.stocks[k] = s
		}
	}
	f.Version++
	result := fulfillmentResponse(f, false)
	m.cancelIdempotency[key] = result
	writeJSON(w, 200, result)
}
func (m *Mock) evidenceURL(w http.ResponseWriter, id, evidenceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fulfillments[id]
	if f == nil || f.Evidence[evidenceID].ID == "" {
		writeError(w, 404, "EVIDENCE_NOT_FOUND", "evidence not found", nil)
		return
	}
	expires := time.Now().UTC().Add(5 * time.Minute)
	writeJSON(w, 200, map[string]any{"url": "https://routefit.invalid/evidence/" + url.PathEscape(evidenceID), "expiresAt": expires})
}
func (m *Mock) statement(w http.ResponseWriter, date string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lines []FeeLine
	totals := map[string]int64{}
	for _, f := range m.feeLines {
		if f.ServiceDate == date {
			lines = append(lines, f)
			totals[f.OwnerRef+"|"+f.FeeCode] += f.Amount.AmountMinor
		}
	}
	counts := map[string]int64{}
	for _, f := range lines {
		counts[f.OwnerRef+"|"+f.FeeCode]++
	}
	var summary []map[string]any
	for k, amount := range totals {
		bits := strings.Split(k, "|")
		summary = append(summary, map[string]any{"ownerRef": bits[0], "feeCode": bits[1], "amountMinor": amount, "lineCount": counts[k]})
	}
	writeJSON(w, 200, map[string]any{"serviceDate": date, "statementVersion": 1, "feeLines": lines, "summary": summary})
}

// Deliver emits an event to a callback with the contract's HMAC signature.
func (m *Mock) Deliver(ctx context.Context, callback string, e Event, opt DeliveryOptions) error {
	return m.DeliverMany(ctx, callback, []Event{e}, opt)
}
func (m *Mock) DeliverMany(ctx context.Context, callback string, events []Event, opt DeliveryOptions) error {
	if opt.Reverse {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}
	for _, e := range events {
		if err := m.deliverOne(ctx, callback, e, opt); err != nil {
			return err
		}
		if opt.Duplicate {
			if err := m.deliverOne(ctx, callback, e, opt); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Mock) deliverOne(ctx context.Context, callback string, e Event, opt DeliveryOptions) error {
	if opt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Timeout)
		defer cancel()
	}
	if opt.Delay > 0 {
		select {
		case <-time.After(opt.Delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if e.SchemaVersion == "" {
		e.SchemaVersion = "v1"
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	ts := time.Now().Unix()
	if opt.ExpiredTimestamp {
		ts -= 301
	}
	signature := m.signature(ts, body)
	if opt.InvalidSignature {
		signature = "v1=0000"
	}
	attempts := 1
	if opt.Retry {
		attempts = 3
	}
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, callback, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-RouteFit-Event-Id", e.ID)
			req.Header.Set("X-RouteFit-Timestamp", strconv.FormatInt(ts, 10))
			req.Header.Set("X-RouteFit-Signature", signature)
			resp, callErr := http.DefaultClient.Do(req)
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return nil
				}
				err = fmt.Errorf("callback status %d", resp.StatusCode)
			} else {
				err = callErr
			}
		}
		if err == nil {
			return nil
		}
		if i+1 < attempts {
			select {
			case <-time.After(time.Duration(i+1) * 10 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}
func (m *Mock) signature(timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(m.config.CallbackSecret))
	fmt.Fprintf(mac, "%d.", timestamp)
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}
func (m *Mock) id(prefix string) string            { m.next++; return fmt.Sprintf("%s-%d", prefix, m.next) }
func stockKey(owner, warehouse, sku string) string { return owner + "|" + warehouse + "|" + sku }
func evidenceList(items map[string]Evidence) []Evidence {
	result := make([]Evidence, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	current := int64(0)
	if d, ok := details.(map[string]any); ok {
		if v, ok := d["currentVersion"].(int64); ok {
			current = v
		}
	}
	writeJSON(w, status, apiError{Code: code, Message: message, Details: details, Retryable: status >= 500, RequestID: "routefitmock", CurrentVersion: current})
}
func sameLines(a, b []line) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].SKU != b[i].SKU || a[i].Quantity != b[i].Quantity || a[i].Ref != b[i].Ref {
			return false
		}
	}
	return true
}
func fulfillmentResponse(f *fulfillment, duplicate bool) map[string]any {
	return map[string]any{"fulfillmentId": f.ID, "version": f.Version, "externalOrderId": f.Request.ExternalOrderID, "state": f.State, "duplicate": duplicate, "packages": []any{}, "trackingNumber": "", "latestEventId": ""}
}
func writeFulfillment(w http.ResponseWriter, f *fulfillment, duplicate bool) {
	writeJSON(w, http.StatusCreated, fulfillmentResponse(f, duplicate))
}
