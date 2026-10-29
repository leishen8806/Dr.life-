package iam

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidLanguage = errors.New("invalid account language")
	ErrAccountNotFound = errors.New("account not found")
)

// AccountProfile contains only the current account facts permitted in a me response.
type AccountProfile struct {
	PublicID         string
	Role             string
	Lang             *string
	NeedsLanguage    bool
	Status           string
	LangRegisteredAt *time.Time
}

// AccountDevice deliberately excludes the client supplied device identifier.
type AccountDevice struct {
	PublicID    string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	IsCurrent   bool
}

// AccountProfileService owns IAM account language and safe device views.
type AccountProfileService struct{ DB *pgxpool.Pool }

func (s AccountProfileService) SetLanguage(ctx context.Context, accountID, lang string) error {
	if !validLanguage(lang) {
		return ErrInvalidLanguage
	}
	var ignored int64
	err := s.DB.QueryRow(ctx, `UPDATE iam.account SET lang=$1,lang_registered_at=COALESCE(lang_registered_at,clock_timestamp()),updated_at=clock_timestamp() WHERE public_id=$2 RETURNING id`, lang, accountID).Scan(&ignored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountNotFound
	}
	return err
}

func (s AccountProfileService) Me(ctx context.Context, accountID string) (AccountProfile, error) {
	var profile AccountProfile
	err := s.DB.QueryRow(ctx, `SELECT public_id,role,lang,lang_registered_at,status FROM iam.account WHERE public_id=$1`, accountID).
		Scan(&profile.PublicID, &profile.Role, &profile.Lang, &profile.LangRegisteredAt, &profile.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountProfile{}, ErrAccountNotFound
	}
	if err != nil {
		return AccountProfile{}, err
	}
	profile.Role = gatewayRole(profile.Role)
	profile.NeedsLanguage = profile.LangRegisteredAt == nil
	return profile, nil
}

func (s AccountProfileService) Devices(ctx context.Context, accountID, sessionID string) ([]AccountDevice, error) {
	rows, err := s.DB.Query(ctx, `SELECT d.public_id,d.first_seen_at,d.last_seen_at,COALESCE(d.device_id=(SELECT s.device_id FROM iam.session s WHERE s.public_id=$2 AND s.account_id=d.account_id),false) FROM iam.account_device d JOIN iam.account a ON a.id=d.account_id WHERE a.public_id=$1 ORDER BY d.last_seen_at DESC`, accountID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]AccountDevice, 0)
	for rows.Next() {
		var device AccountDevice
		if err := rows.Scan(&device.PublicID, &device.FirstSeenAt, &device.LastSeenAt, &device.IsCurrent); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

// GetAccountLanguage is the narrow read-only fact used by notifications.
func (s AccountProfileService) GetAccountLanguage(ctx context.Context, accountID string) (lang string, registered bool, err error) {
	var value *string
	var registeredAt *time.Time
	err = s.DB.QueryRow(ctx, `SELECT lang,lang_registered_at FROM iam.account WHERE public_id=$1`, accountID).Scan(&value, &registeredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrAccountNotFound
	}
	if err != nil {
		return "", false, err
	}
	if value != nil {
		lang = *value
	}
	return lang, registeredAt != nil, nil
}

func validLanguage(lang string) bool {
	return lang == "zh" || lang == "en" || lang == "km"
}
