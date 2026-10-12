package ledger

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateRequest(t *testing.T) {
	valid := PostRequest{
		Type: "topup", Amount: 10, IdemKey: "topup:order:1", ActorID: 1,
		Postings: []Posting{{AccountID: 1, Side: "DEBIT", Amount: 10}, {AccountID: 2, Side: "CREDIT", Amount: 10}},
	}
	if err := validateRequest(valid); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(*PostRequest)
		want error
	}{
		{"zero amount", func(r *PostRequest) { r.Amount = 0 }, ErrInvalidAmount},
		{"negative amount", func(r *PostRequest) { r.Amount = -1 }, ErrInvalidAmount},
		{"one posting", func(r *PostRequest) { r.Postings = r.Postings[:1] }, ErrInvalidRequest},
		{"duplicate account", func(r *PostRequest) { r.Postings[1].AccountID = 1 }, ErrInvalidRequest},
		{"unknown type", func(r *PostRequest) { r.Type = "unknown" }, ErrInvalidRequest},
		{"empty idem key", func(r *PostRequest) { r.IdemKey = "" }, ErrInvalidRequest},
		{"invalid idem key", func(r *PostRequest) { r.IdemKey = "topup:order:1;drop" }, ErrInvalidRequest},
		{"invalid category", func(r *PostRequest) { r.Postings[1].Category = "unknown" }, ErrInvalidRequest},
		{"long idem key", func(r *PostRequest) { r.IdemKey = "topup:order:" + string(make([]byte, 190)) }, ErrInvalidRequest},
		{"amount mismatch", func(r *PostRequest) { r.Amount = 11 }, ErrUnbalanced},
		{"unbalanced", func(r *PostRequest) { r.Postings[1].Amount = 9 }, ErrUnbalanced},
		{"overflow", func(r *PostRequest) {
			r.Amount = math.MaxInt64
			r.Postings = []Posting{{1, "DEBIT", math.MaxInt64, "", ""}, {2, "DEBIT", 1, "", ""}, {3, "CREDIT", math.MaxInt64, "", ""}}
		}, ErrInvalidAmount},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := valid
			request.Postings = append([]Posting(nil), valid.Postings...)
			tc.edit(&request)
			if err := validateRequest(request); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestSortedAccountIDs(t *testing.T) {
	got := sortedAccountIDs([]Posting{{AccountID: 9}, {AccountID: 2}, {AccountID: 9}, {AccountID: 4}})
	want := []int64{2, 4, 9}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted IDs=%v, want %v", got, want)
	}
}

func TestCheckedAdd(t *testing.T) {
	if _, ok := checkedAdd(math.MaxInt64, 1); ok {
		t.Fatal("positive overflow accepted")
	}
	if _, ok := checkedAdd(math.MinInt64, -1); ok {
		t.Fatal("negative overflow accepted")
	}
}

func TestRetryableDatabaseErrors(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		if !isRetryable(&pgconn.PgError{Code: code}) {
			t.Errorf("SQLSTATE %s should be retryable", code)
		}
	}
	if isRetryable(&pgconn.PgError{Code: "23514"}) {
		t.Fatal("constraint failures must not be retried")
	}
}
