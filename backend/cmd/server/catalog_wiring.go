package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	api "github.com/leishen8806/Dr.life-/backend/internal/api"
	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
)

// catalogQuerier 是适配器所需的最小只读能力；*pgxpool.Pool 满足。
type catalogQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// catalogDirectory 在平台侧实现 catalog 的出站端口（MerchantDirectory）以及
// api.MerchantResolver：跨 schema 读取集中在进程装配处，catalog 模块只消费端口，
// 因此 catalog 自身仍然只访问 catalog schema（AGENTS.md 规则 8）。
type catalogDirectory struct {
	db catalogQuerier
}

var (
	_ catalog.MerchantDirectory = catalogDirectory{}
	_ api.MerchantResolver      = catalogDirectory{}
)

// SellableMerchants 返回对买家可见（可售）的商户 id：仅 status = active。
func (d catalogDirectory) SellableMerchants(ctx context.Context) ([]int64, error) {
	rows, err := d.db.Query(ctx, `SELECT id FROM iam.merchant WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MerchantStatus 返回商户状态；未注册（无行）按 catalog.ErrMerchantUnknown 处理。
func (d catalogDirectory) MerchantStatus(ctx context.Context, merchantID int64) (catalog.MerchantStatus, error) {
	var status string
	err := d.db.QueryRow(ctx, `SELECT status FROM iam.merchant WHERE id = $1`, merchantID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", catalog.ErrMerchantUnknown
	}
	if err != nil {
		return "", err
	}
	return catalog.MerchantStatus(status), nil
}

// MerchantID 把网关身份中的 iam.account.public_id 解析为商户内部 id。
// 账号不存在、已停用、或未绑定商户时 found = false（不区分原因，避免探测账号）。
func (d catalogDirectory) MerchantID(ctx context.Context, accountID string) (int64, bool, error) {
	var merchantID *int64
	err := d.db.QueryRow(ctx, `
		SELECT merchant_id
		FROM iam.account
		WHERE public_id = $1 AND status = 'active' AND merchant_id IS NOT NULL`, accountID).Scan(&merchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if merchantID == nil {
		return 0, false, nil
	}
	return *merchantID, true, nil
}
