package trade

import (
	"context"
	"errors"
	"math"
	"testing"
)

type stubAccounts struct{ status string }

func (s stubAccounts) Status(context.Context, int64) (string, error) { return s.status, nil }

type stubCatalog struct {
	lines        map[string]CatalogLine
	reservations map[string]bool
	releases     int
	releaseErr   error
	commitErr    error
	lastInput    []OrderLineInput
	resolveCalls int
	commits      []string
}

func (s *stubCatalog) ResolveAndReserve(_ context.Context, key string, input []OrderLineInput) ([]CatalogLine, error) {
	s.resolveCalls++
	s.lastInput = append([]OrderLineInput(nil), input...)
	result := make([]CatalogLine, 0, len(input))
	for _, line := range input {
		snapshot, ok := s.lines[line.SKUPublicID]
		if !ok {
			return nil, ErrInsufficientAvailability
		}
		snapshot.Quantity = line.Quantity
		result = append(result, snapshot)
	}
	if s.reservations == nil {
		s.reservations = map[string]bool{}
	}
	s.reservations[key] = true
	return result, nil
}

func (s *stubCatalog) Release(_ context.Context, key string) error {
	if s.releaseErr != nil {
		return s.releaseErr
	}
	if s.reservations[key] {
		delete(s.reservations, key)
		s.releases++
	}
	return nil
}
func (s *stubCatalog) Commit(_ context.Context, key string) error {
	s.commits = append(s.commits, key)
	return s.commitErr
}

type stubOrders struct {
	created     CreateOrder
	queued      []string
	reservation Reservation
	marked      int
	createErr   error
	paymentKey  string
	paymentOK   bool
	paymentErr  error
	outbox      []ReservationAction
	completed   []int64
	failed      []int64
	commitDone  []string
}

func (s *stubOrders) FindByIdempotency(_ context.Context, _ int64, _ string, hash string) (CreateOrder, bool, error) {
	if s.created.PublicID == "" {
		return CreateOrder{}, false, nil
	}
	if s.created.RequestHash != hash {
		return CreateOrder{}, false, ErrIdempotencyConflict
	}
	return s.created, true, nil
}

func (s *stubOrders) Create(_ context.Context, key, hash string, input CreateOrder) (CreateOrder, error) {
	if s.createErr != nil {
		return CreateOrder{}, s.createErr
	}
	if s.created.PublicID != "" {
		if s.created.RequestHash != hash {
			return CreateOrder{}, ErrIdempotencyConflict
		}
		return s.created, nil
	}
	input.PublicID, input.IdempotencyKey, input.RequestHash = "buyer-order", key, hash
	s.created = input
	return input, nil
}
func (s *stubOrders) QueueReservationRelease(_ context.Context, key, reason string) error {
	s.queued = append(s.queued, key+":"+reason)
	return nil
}
func (s *stubOrders) ClaimReservationRelease(context.Context, string) (Reservation, bool, error) {
	if s.reservation.Released {
		return s.reservation, true, nil
	}
	s.reservation.Released = true
	s.marked++
	return s.reservation, true, nil
}
func (s *stubOrders) ClaimReservationActions(context.Context, int) ([]ReservationAction, error) {
	return append([]ReservationAction(nil), s.outbox...), nil
}
func (s *stubOrders) CompleteReservationAction(_ context.Context, id int64) error {
	s.completed = append(s.completed, id)
	return nil
}
func (s *stubOrders) FailReservationAction(_ context.Context, id int64) error {
	s.failed = append(s.failed, id)
	return nil
}
func (s *stubOrders) CompleteReservationCommit(_ context.Context, key string) error {
	s.commitDone = append(s.commitDone, key)
	return nil
}
func (s *stubOrders) ConfirmPayment(context.Context, string, int64, EventSource) (string, bool, error) {
	return s.paymentKey, s.paymentOK, s.paymentErr
}

type scopedOrders struct {
	stubOrders
	buyer    BuyerOrderView
	merchant MerchantOrderView
}

func (s *scopedOrders) FindBuyerOrder(_ context.Context, buyerID int64, publicID string) (BuyerOrderView, error) {
	if buyerID != s.buyer.BuyerAccountID || publicID != s.buyer.PublicID {
		return BuyerOrderView{}, ErrOrderNotVisible
	}
	return s.buyer, nil
}
func (s *scopedOrders) FindMerchantOrder(_ context.Context, merchantID int64, publicID string) (MerchantOrderView, error) {
	if merchantID != s.merchant.MerchantID || publicID != s.merchant.PublicID {
		return MerchantOrderView{}, ErrOrderNotVisible
	}
	return s.merchant, nil
}

func catalogFixture() *stubCatalog {
	return &stubCatalog{lines: map[string]CatalogLine{
		"A": {SKUPublicID: "A", MerchantID: 10, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 100}}, OnSale: true},
		"B": {SKUPublicID: "B", MerchantID: 10, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 200}}, OnSale: true},
		"C": {SKUPublicID: "C", MerchantID: 20, Tiers: []Tier{{MinQuantity: 1, UnitPriceE4: 300}}, OnSale: true},
	}}
}

func TestPlaceUsesServerFactsMergesDuplicateSKUAndIsIdempotent(t *testing.T) {
	catalog := catalogFixture()
	store := &stubOrders{}
	svc := NewService(store, stubAccounts{status: "active"}, catalog)
	input := []OrderLineInput{{SKUPublicID: "C", Quantity: 1}, {SKUPublicID: "A", Quantity: 1}, {SKUPublicID: "A", Quantity: 1}}
	order, err := svc.Place(context.Background(), 1, "request-1", input)
	if err != nil {
		t.Fatal(err)
	}
	if order.TotalE4 != 500 || len(order.Children) != 2 {
		t.Fatalf("order=%+v", order)
	}
	if got := catalog.lastInput; len(got) != 2 || got[0].SKUPublicID != "A" || got[0].Quantity != 2 || got[1].SKUPublicID != "C" {
		t.Fatalf("catalog input=%+v, want sorted merged SKUs", got)
	}
	replayed, err := svc.Place(context.Background(), 1, "request-1", input)
	if err != nil || replayed.PublicID != order.PublicID || len(catalog.reservations) != 1 || catalog.resolveCalls != 1 {
		t.Fatalf("replay=%+v err=%v reservations=%v resolve_calls=%d", replayed, err, catalog.reservations, catalog.resolveCalls)
	}
	if _, err := svc.Place(context.Background(), 1, "request-1", []OrderLineInput{{SKUPublicID: "A", Quantity: 3}}); !errors.Is(err, ErrIdempotencyConflict) || !catalog.reservations["1:request-1"] {
		t.Fatalf("conflicting replay err=%v reservations=%v", err, catalog.reservations)
	}
}

func TestPlaceRejectsInactiveMissingKeyAndUnavailableWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name, status, key, sku string
		want                   error
	}{
		{"missing key", "active", "", "A", ErrIdempotencyRequired},
		{"inactive", "frozen", "key", "A", ErrBuyerInactive},
		{"unavailable", "active", "key", "missing", ErrInsufficientAvailability},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubOrders{}
			_, err := NewService(store, stubAccounts{tc.status}, catalogFixture()).Place(context.Background(), 1, tc.key, []OrderLineInput{{SKUPublicID: tc.sku, Quantity: 1}})
			if !errors.Is(err, tc.want) || store.created.PublicID != "" {
				t.Fatalf("err=%v created=%+v", err, store.created)
			}
		})
	}
}

func TestPlaceRejectsInvalidServerPricingAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     CatalogLine
		quantity int64
	}{
		{"mismatched sku", CatalogLine{SKUPublicID: "B", MerchantID: 1, Tiers: []Tier{{1, 100}}, OnSale: true}, 1},
		{"off sale", CatalogLine{MerchantID: 1, Tiers: []Tier{{1, 100}}}, 1},
		{"negative tier", CatalogLine{MerchantID: 1, Tiers: []Tier{{1, -100}}, OnSale: true}, 1},
		{"duplicate tier", CatalogLine{MerchantID: 1, Tiers: []Tier{{1, 100}, {1, 200}}, OnSale: true}, 1},
		{"sub cent", CatalogLine{MerchantID: 1, Tiers: []Tier{{1, 101}}, OnSale: true}, 1},
		{"multiply overflow", CatalogLine{MerchantID: 1, Tiers: []Tier{{1, 200}}, OnSale: true}, math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := tc.line
			if line.SKUPublicID == "" {
				line.SKUPublicID = "A"
			}
			catalog := &stubCatalog{lines: map[string]CatalogLine{"A": line}}
			_, err := NewService(&stubOrders{}, stubAccounts{"active"}, catalog).Place(context.Background(), 1, "key", []OrderLineInput{{"A", tc.quantity}})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if len(catalog.reservations) != 0 {
				t.Fatalf("reservation leaked: %v", catalog.reservations)
			}
		})
	}
}

func TestReleaseReservationsIsIdempotentAndQueuesFailures(t *testing.T) {
	store := &stubOrders{reservation: Reservation{Key: "1:key", Status: Cancelled}}
	catalog := &stubCatalog{reservations: map[string]bool{"1:key": true}}
	svc := NewService(store, stubAccounts{"active"}, catalog)
	if err := svc.ReleaseReservations(context.Background(), "buyer-order"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReleaseReservations(context.Background(), "buyer-order"); err != nil || catalog.releases != 1 || store.marked != 1 {
		t.Fatalf("second release err=%v releases=%d marked=%d", err, catalog.releases, store.marked)
	}

	store = &stubOrders{reservation: Reservation{Key: "1:key", Status: Cancelled}}
	catalog = &stubCatalog{releaseErr: errors.New("catalog unavailable")}
	err := NewService(store, stubAccounts{"active"}, catalog).ReleaseReservations(context.Background(), "buyer-order")
	if err == nil || len(store.queued) != 1 || store.queued[0] != "1:key:release_failed" {
		t.Fatalf("err=%v queued=%v", err, store.queued)
	}
}

func TestCreateFailureQueuesReleaseWhenImmediateReleaseFails(t *testing.T) {
	store := &stubOrders{createErr: errors.New("db unavailable")}
	catalog := catalogFixture()
	catalog.releaseErr = errors.New("catalog unavailable")
	_, err := NewService(store, stubAccounts{"active"}, catalog).Place(context.Background(), 1, "key", []OrderLineInput{{"A", 1}})
	if err == nil || len(store.queued) != 1 || store.queued[0] != "1:key:create_failed" {
		t.Fatalf("err=%v queued=%v", err, store.queued)
	}
}

func TestConfirmPaymentCommitsReservationOnFirstCallAndReplay(t *testing.T) {
	store := &stubOrders{paymentKey: "1:key", paymentOK: true}
	catalog := catalogFixture()
	svc := NewService(store, stubAccounts{"active"}, catalog)
	if applied, err := svc.ConfirmPayment(context.Background(), "buyer-order", 500, EventSource{Type: SourceKESSResult, Reference: "payment"}); err != nil || !applied {
		t.Fatalf("first payment applied=%v err=%v", applied, err)
	}
	store.paymentOK = false
	if applied, err := svc.ConfirmPayment(context.Background(), "buyer-order", 500, EventSource{Type: SourceKESSResult, Reference: "payment"}); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	if len(catalog.commits) != 2 || catalog.commits[0] != "1:key" || catalog.commits[1] != "1:key" || len(store.commitDone) != 2 {
		t.Fatalf("commits=%v completed=%v", catalog.commits, store.commitDone)
	}
}

func TestConfirmPaymentSurfacesLostReservationForRefund(t *testing.T) {
	store := &stubOrders{paymentKey: "1:key", paymentOK: true}
	catalog := catalogFixture()
	catalog.commitErr = ErrReservationReleased
	applied, err := NewService(store, stubAccounts{"active"}, catalog).ConfirmPayment(context.Background(), "buyer-order", 500, EventSource{Type: SourceKESSResult, Reference: "payment"})
	if !applied || !errors.Is(err, ErrReservationReleased) {
		t.Fatalf("applied=%v err=%v, want applied payment with ErrReservationReleased refund signal", applied, err)
	}
}

func TestProcessReservationActionsCompletesSuccessAndReschedulesFailure(t *testing.T) {
	store := &stubOrders{outbox: []ReservationAction{{ID: 1, Key: "1:ok", Action: "release"}, {ID: 3, Key: "1:commit", Action: "commit"}}}
	catalog := catalogFixture()
	catalog.reservations = map[string]bool{"1:ok": true, "1:fail": true}
	if err := NewService(store, stubAccounts{"active"}, catalog).ProcessReservationActions(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	store.outbox = []ReservationAction{{ID: 2, Key: "1:fail", Action: "release"}}
	catalog.releaseErr = errors.New("catalog unavailable")
	if err := NewService(store, stubAccounts{"active"}, catalog).ProcessReservationActions(context.Background(), 1); err == nil {
		t.Fatal("expected release failure")
	}
	if len(store.completed) != 2 || store.completed[0] != 1 || store.completed[1] != 3 || len(store.failed) != 1 || store.failed[0] != 2 || len(catalog.commits) != 1 || catalog.commits[0] != "1:commit" {
		t.Fatalf("completed=%v failed=%v commits=%v", store.completed, store.failed, catalog.commits)
	}
}

func TestReadScopesDoNotExposeOtherOrders(t *testing.T) {
	store := &scopedOrders{
		buyer:    BuyerOrderView{PublicID: "buyer-order", BuyerAccountID: 11, TotalE4: 100},
		merchant: MerchantOrderView{PublicID: "merchant-order", MerchantID: 22, TotalE4: 100},
	}
	svc := NewService(store, stubAccounts{status: "active"}, catalogFixture())
	if _, err := svc.BuyerOrder(context.Background(), 12, "buyer-order"); !errors.Is(err, ErrOrderNotVisible) {
		t.Fatalf("foreign buyer error=%v", err)
	}
	if _, err := svc.MerchantOrder(context.Background(), 23, "merchant-order"); !errors.Is(err, ErrOrderNotVisible) {
		t.Fatalf("foreign merchant error=%v", err)
	}
	if got, err := svc.MerchantOrder(context.Background(), 22, "merchant-order"); err != nil || got.TotalE4 != 100 {
		t.Fatalf("own merchant order=%+v err=%v", got, err)
	}
}
