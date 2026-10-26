package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type AnchorRecord struct {
	Day      string `json:"day"`
	LastID   int64  `json:"last_id"`
	LastHash string `json:"last_hash"`
}

type AnchorSink interface {
	Put(context.Context, AnchorRecord) error
}

type FileAnchorSink struct{ Path string }

func (sink FileAnchorSink) Put(_ context.Context, record AnchorRecord) error {
	if sink.Path == "" {
		return errors.New("anchor file path is required")
	}
	if err := os.MkdirAll(filepath.Dir(sink.Path), 0o700); err != nil {
		return fmt.Errorf("create anchor directory: %w", err)
	}
	file, err := os.OpenFile(sink.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open anchor file: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	if err := json.NewEncoder(writer).Encode(record); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Store) AnchorDay(ctx context.Context, day time.Time, sink AnchorSink) (AnchorRecord, error) {
	if s == nil || s.DB == nil || sink == nil {
		return AnchorRecord{}, errors.New("audit anchor is not configured")
	}
	day = day.UTC()
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	var id int64
	var hash []byte
	if err := s.DB.QueryRow(ctx, `SELECT id,hash FROM audit.log WHERE ts >= $1 AND ts < $2 ORDER BY id DESC LIMIT 1`, start, end).Scan(&id, &hash); err != nil {
		return AnchorRecord{}, fmt.Errorf("load daily audit tail: %w", err)
	}
	record := AnchorRecord{Day: start.Format(time.DateOnly), LastID: id, LastHash: hashHex(hash)}
	if err := sink.Put(ctx, record); err != nil {
		return AnchorRecord{}, fmt.Errorf("write external audit anchor: %w", err)
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO audit.anchor (day,last_id,last_hash) VALUES ($1,$2,$3) ON CONFLICT (day) DO NOTHING`, start, id, hash); err != nil {
		return AnchorRecord{}, fmt.Errorf("record audit anchor: %w", err)
	}
	return record, nil
}

func (s *Store) CheckAnchorFreshness(ctx context.Context, now time.Time) (bool, error) {
	var anchoredAt *time.Time
	if err := s.DB.QueryRow(ctx, `SELECT max(anchored_at) FROM audit.anchor`).Scan(&anchoredAt); err != nil {
		return false, err
	}
	missing := anchoredAt == nil || now.Sub(*anchoredAt) > 24*time.Hour
	if s.Metrics != nil {
		s.Metrics.AuditAnchorMissing(missing)
	}
	return missing, nil
}
