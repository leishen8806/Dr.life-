package audit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var GenesisHash = make([]byte, sha256.Size)

type Action string

const (
	ActionLogin              Action = "auth.login"
	ActionStepUp             Action = "auth.step_up"
	ActionPermissionChanged  Action = "permission.changed"
	ActionAccountChanged     Action = "account.changed"
	ActionMerchantVerified   Action = "merchant.verified"
	ActionProductModerated   Action = "product.moderated"
	ActionWithdrawalReviewed Action = "withdrawal.reviewed"
	ActionWithdrawalPaid     Action = "withdrawal.paid"
	ActionDisputeResolved    Action = "dispute.resolved"
	ActionLogisticsChanged   Action = "logistics.changed"
	ActionIntegrationChanged Action = "integration.changed"
	ActionRuleChanged        Action = "rule.changed"
)

var validActions = map[Action]struct{}{
	ActionLogin: {}, ActionStepUp: {}, ActionPermissionChanged: {}, ActionAccountChanged: {},
	ActionMerchantVerified: {}, ActionProductModerated: {}, ActionWithdrawalReviewed: {},
	ActionWithdrawalPaid: {}, ActionDisputeResolved: {}, ActionLogisticsChanged: {},
	ActionIntegrationChanged: {}, ActionRuleChanged: {},
}

type Entry struct {
	Time      time.Time
	ActorID   int64
	ActorRole string
	Action    Action
	Target    string
	Detail    json.RawMessage
	IP        string
	RequestID string
}

type Record struct {
	ID        int64           `json:"id"`
	Time      time.Time       `json:"ts"`
	ActorID   int64           `json:"actor_id"`
	ActorRole string          `json:"actor_role"`
	Action    Action          `json:"action"`
	Target    string          `json:"target"`
	Detail    json.RawMessage `json:"detail"`
	IP        string          `json:"ip,omitempty"`
	RequestID string          `json:"request_id"`
	PrevHash  []byte          `json:"-"`
	Hash      []byte          `json:"-"`
}

type Writer interface {
	Append(context.Context, Entry) (Record, error)
}

type Metrics interface {
	AuditAppend(bool)
	AuditVerifyFailure()
	AuditAnchorMissing(bool)
}

type Store struct {
	DB      *pgxpool.Pool
	Metrics Metrics
	Now     func() time.Time
}

func (s *Store) Append(ctx context.Context, entry Entry) (Record, error) {
	if s == nil || s.DB == nil {
		return Record{}, errors.New("audit database is unavailable")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		s.recordAppend(false)
		return Record{}, fmt.Errorf("begin audit append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := s.appendTx(ctx, tx, entry)
	if err == nil {
		err = tx.Commit(ctx)
	}
	s.recordAppend(err == nil)
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) appendTx(ctx context.Context, tx pgx.Tx, entry Entry) (Record, error) {
	canonical, err := validateAndCanonicalize(&entry, s.now())
	if err != nil {
		return Record{}, err
	}
	var previousID *int64
	previousHash := make([]byte, sha256.Size)
	if err := tx.QueryRow(ctx, `SELECT last_id, last_hash FROM audit.lock_chain_head()`).Scan(&previousID, &previousHash); err != nil {
		return Record{}, fmt.Errorf("lock audit chain head: %w", err)
	}
	hash := computeHash(previousHash, entry, canonical)
	var record Record
	err = tx.QueryRow(ctx, `
		INSERT INTO audit.log (ts, actor_id, actor_role, action, target, detail, ip, request_id, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, ts, actor_id, actor_role, action, target, detail, coalesce(host(ip), ''), request_id, prev_hash, hash`,
		entry.Time, entry.ActorID, entry.ActorRole, entry.Action, entry.Target, canonical, nullableIP(entry.IP), entry.RequestID, previousHash, hash,
	).Scan(&record.ID, &record.Time, &record.ActorID, &record.ActorRole, &record.Action, &record.Target, &record.Detail, &record.IP, &record.RequestID, &record.PrevHash, &record.Hash)
	if err != nil {
		return Record{}, fmt.Errorf("insert audit log: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT audit.advance_chain_head($1,$2,$3)`, previousHash, record.ID, hash); err != nil {
		return Record{}, fmt.Errorf("advance audit chain head: %w", err)
	}
	return record, nil
}

func validateAndCanonicalize(entry *Entry, now time.Time) ([]byte, error) {
	if _, ok := validActions[entry.Action]; !ok {
		return nil, fmt.Errorf("invalid audit action %q", entry.Action)
	}
	entry.ActorRole = strings.TrimSpace(entry.ActorRole)
	entry.Target = strings.TrimSpace(entry.Target)
	entry.RequestID = strings.TrimSpace(entry.RequestID)
	if entry.ActorID <= 0 || entry.ActorRole == "" || entry.Target == "" || entry.RequestID == "" {
		return nil, errors.New("audit actor, role, target and request ID are required")
	}
	if entry.IP != "" && net.ParseIP(entry.IP) == nil {
		return nil, errors.New("audit IP is invalid")
	}
	if entry.Time.IsZero() {
		entry.Time = now
	}
	entry.Time = entry.Time.UTC().Truncate(time.Microsecond)
	return sanitizeDetail(entry.Detail)
}

func computeHash(previous []byte, entry Entry, canonical []byte) []byte {
	h := sha256.New()
	writeField := func(value []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(value)
	}
	writeField(previous)
	writeField([]byte(entry.Time.UTC().Format(time.RFC3339Nano)))
	writeField([]byte(fmt.Sprintf("%d", entry.ActorID)))
	writeField([]byte(entry.ActorRole))
	writeField([]byte(entry.Action))
	writeField([]byte(entry.Target))
	writeField(canonical)
	writeField([]byte(entry.IP))
	writeField([]byte(entry.RequestID))
	return h.Sum(nil)
}

func nullableIP(ip string) any {
	if ip == "" {
		return nil
	}
	return ip
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) recordAppend(ok bool) {
	if s.Metrics != nil {
		s.Metrics.AuditAppend(ok)
	}
}

func hashHex(value []byte) string { return hex.EncodeToString(value) }
