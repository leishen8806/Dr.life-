package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

type ChainError struct {
	ID     int64
	Reason string
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("audit chain invalid at id %d: %s", e.ID, e.Reason)
}

func (s *Store) Verify(ctx context.Context, fromID, toID int64) error {
	if s == nil || s.DB == nil || fromID < 0 || toID < 0 || (toID != 0 && fromID > toID) {
		return fmt.Errorf("invalid audit verification range")
	}
	rows, err := s.DB.Query(ctx, `
		SELECT id,ts,actor_id,actor_role,action,target,detail,coalesce(host(ip),''),request_id,prev_hash,hash
		FROM audit.log WHERE ($1=0 OR id >= $1) AND ($2=0 OR id <= $2) ORDER BY id`, fromID, toID)
	if err != nil {
		return fmt.Errorf("query audit chain: %w", err)
	}
	defer rows.Close()
	previous := GenesisHash
	var lastID int64
	if fromID > 1 {
		if err := s.DB.QueryRow(ctx, `SELECT id,hash FROM audit.log WHERE id < $1 ORDER BY id DESC LIMIT 1`, fromID).Scan(&lastID, &previous); err != nil {
			return fmt.Errorf("load audit predecessor: %w", err)
		}
	}
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.ID, &record.Time, &record.ActorID, &record.ActorRole, &record.Action, &record.Target, &record.Detail, &record.IP, &record.RequestID, &record.PrevHash, &record.Hash); err != nil {
			return err
		}
		if !bytes.Equal(record.PrevHash, previous) {
			return s.verificationFailure(record.ID, "previous hash mismatch")
		}
		canonical, err := sanitizeDetail(json.RawMessage(record.Detail))
		if err != nil {
			return s.verificationFailure(record.ID, "detail is not canonical JSON")
		}
		entry := Entry{Time: record.Time, ActorID: record.ActorID, ActorRole: record.ActorRole, Action: record.Action, Target: record.Target, IP: record.IP, RequestID: record.RequestID}
		if !bytes.Equal(record.Hash, computeHash(previous, entry, canonical)) {
			return s.verificationFailure(record.ID, "hash mismatch")
		}
		previous = record.Hash
		lastID = record.ID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if toID == 0 {
		var headID *int64
		var headHash []byte
		if err := s.DB.QueryRow(ctx, `SELECT last_id,last_hash FROM audit.chain_head WHERE singleton`).Scan(&headID, &headHash); err != nil {
			return fmt.Errorf("load audit chain head: %w", err)
		}
		if headID == nil {
			if lastID != 0 || !bytes.Equal(previous, GenesisHash) {
				return s.verificationFailure(lastID, "chain head mismatch")
			}
		} else if lastID != *headID || !bytes.Equal(previous, headHash) {
			return s.verificationFailure(*headID, "chain head mismatch")
		}
	}
	return nil
}

func (s *Store) verificationFailure(id int64, reason string) error {
	if s.Metrics != nil {
		s.Metrics.AuditVerifyFailure()
	}
	return &ChainError{ID: id, Reason: reason}
}
