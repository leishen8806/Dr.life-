package iam_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
)

const (
	personPublicID   = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	merchantPublicID = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	buyerPublicID    = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	ownerPublicID    = "01ARZ3NDEKTSV4RRFFQ69G5FAY"
)

func TestIAMMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_iam_test"),
		postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	migrationsDir := migrationDirectory(t)
	if err := goose.Up(database, migrationsDir); err != nil {
		t.Fatal(err)
	}

	t.Run("up down up is reversible", func(t *testing.T) {
		assertIAMTableCount(t, database, 17)
		if err := goose.DownTo(database, migrationsDir, 2); err != nil {
			t.Fatal(err)
		}
		assertIAMTableCount(t, database, 0)
		if err := goose.Up(database, migrationsDir); err != nil {
			t.Fatal(err)
		}
		assertIAMTableCount(t, database, 17)
	})

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	t.Run("identity public ids timestamps and foreign keys", func(t *testing.T) {
		var identityCount, publicIDCount, badTimestamp int
		if err := admin.QueryRow(ctx, `
			SELECT
				count(*) FILTER (WHERE c.column_name = 'id' AND c.is_identity = 'YES'),
				count(*) FILTER (WHERE c.column_name = 'public_id' AND c.is_nullable = 'NO'),
				count(*) FILTER (WHERE c.column_name IN ('created_at','updated_at') AND (c.data_type <> 'timestamp with time zone' OR c.is_nullable <> 'NO' OR c.column_default IS NULL))
			FROM information_schema.columns c WHERE c.table_schema = 'iam'`).Scan(&identityCount, &publicIDCount, &badTimestamp); err != nil {
			t.Fatal(err)
		}
		if identityCount != 14 || publicIDCount != 14 || badTimestamp != 0 {
			t.Fatalf("identity columns=%d, public IDs=%d, bad timestamps=%d", identityCount, publicIDCount, badTimestamp)
		}
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('bad-ulid', 1)`)
		assertSQLState(t, ctx, admin, "23503", `INSERT INTO iam.account (public_id, person_id, role) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA0', 999999, 'buyer')`)
	})

	seedFixtures(t, ctx, admin)

	t.Run("person and account constraints", func(t *testing.T) {
		assertSQLState(t, ctx, admin, "23505", fmt.Sprintf(`INSERT INTO iam.person (public_id, tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA1', 1001)`))
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.person (public_id, tg_user_id) VALUES ('`+personPublicID+`', 1002)`)
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.account (public_id, person_id, role) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA2', id, 'buyer' FROM iam.person WHERE public_id = '`+personPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.account (public_id, person_id, role, status) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA3', id, 'driver', 'frozen' FROM iam.person WHERE public_id = '`+personPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.account (public_id, person_id, role) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA4', id, 'admin' FROM iam.person WHERE public_id = '`+personPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.account (public_id, person_id, role, lang) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FA5', id, 'driver', 'fr' FROM iam.person WHERE public_id = '`+personPublicID+`'`)
	})

	t.Run("bot and binding constraints", func(t *testing.T) {
		insertBot(t, ctx, admin, "01ARZ3NDEKTSV4RRFFQ69G5FA6", "buyer", "buyer_primary", "primary", "active", 1)
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.bot (public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA7','buyer','buyer_second_primary','primary','active','\x01','\x02',2)`)
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.bot (public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAQ','merchant','buyer_primary','standby','active','\x01','\x02',1)`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.bot (public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA8','admin','bad_end','standby','active','\x01','\x02',3)`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.bot (public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA9','driver','bad_status','standby','offline','\x01','\x02',1)`)
		insertAccountBot(t, ctx, admin, "01ARZ3NDEKTSV4RRFFQ69G5FAA")
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.account_bot (public_id,account_id,bot_id) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAB',a.id,b.id FROM iam.account a,iam.bot b WHERE a.public_id='`+buyerPublicID+`' AND b.username='buyer_primary'`)
	})

	t.Run("session indexes exist", func(t *testing.T) {
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname='iam' AND tablename='session' AND indexname IN ('ix_session_account_id','ix_session_expires_at','ix_session_family_id','ix_session_previous_hash','ix_session_active_account')`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 5 {
			t.Fatalf("session index count=%d, want 5", count)
		}
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='iam' AND data_type='bytea' AND (table_name,column_name) IN (('person','phone_enc'),('merchant','phone_enc'),('merchant','address_enc'),('merchant','contact_enc'),('bot','token_enc'),('bot','webhook_secret_enc'),('session','refresh_token_hash'),('session','previous_hash'),('merchant_staff','phone_enc'),('merchant_staff','phone_hash'),('admin_user','totp_secret_enc'))`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 11 {
			t.Fatalf("sensitive bytea column count=%d, want 11", count)
		}
	})

	t.Run("merchant staff and role permission constraints", func(t *testing.T) {
		insertStaff(t, ctx, admin, "01ARZ3NDEKTSV4RRFFQ69G5FAC", "invite-1", "\\x1111")
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.merchant_staff (public_id,merchant_id,name,phone_enc,phone_hash,role,status,invite_code) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAD',id,'B','\x02','\x1111','sales','invited','invite-2' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.merchant_staff (public_id,merchant_id,name,phone_enc,phone_hash,role,status,invite_code) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAE',id,'C','\x03','\x2222','sales','invited','invite-1' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.merchant_staff (public_id,merchant_id,name,phone_enc,phone_hash,role,status,invite_code) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAF',id,'D','\x04','\x3333','owner','invited','invite-3' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.merchant_staff (public_id,merchant_id,name,phone_enc,phone_hash,role,status,invite_code) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAR',id,'E','\x05','\x4444','sales','disabled','invite-4' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.merchant_role_perm (merchant_id,role,module,level) SELECT id,'manager','staff','edit' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.merchant_role_perm (merchant_id,role,module,level) SELECT id,'manager','unknown','view' FROM iam.merchant WHERE public_id='`+merchantPublicID+`'`)
	})

	t.Run("active links admin permissions and actors are unique", func(t *testing.T) {
		insertAccountLink(t, ctx, admin, "01ARZ3NDEKTSV4RRFFQ69G5FAG")
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.account_link (public_id,buyer_account_id,merchant_account_id) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAH',b.id,m.id FROM iam.account b,iam.account m WHERE b.public_id='`+buyerPublicID+`' AND m.public_id='`+ownerPublicID+`'`)
		if _, err := admin.Exec(ctx, `UPDATE iam.account_link SET unlinked_at=now() WHERE public_id='01ARZ3NDEKTSV4RRFFQ69G5FAG'`); err != nil {
			t.Fatal(err)
		}
		insertAccountLink(t, ctx, admin, "01ARZ3NDEKTSV4RRFFQ69G5FAS")
		assertSQLState(t, ctx, admin, "23514", `UPDATE iam.admin_role_perm SET level='view' WHERE role='super' AND module='dash'`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.admin_user (public_id,name,email,password_hash,role) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAJ','A','a@example.test','hash','owner')`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.admin_role_perm (role,module,level) VALUES ('ops','unknown','view')`)
		if _, err := admin.Exec(ctx, `INSERT INTO iam.actor (public_id,kind,ref_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAK','system',1)`); err != nil {
			t.Fatal(err)
		}
		assertSQLState(t, ctx, admin, "23505", `INSERT INTO iam.actor (public_id,kind,ref_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAM','system',1)`)
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.actor (public_id,kind,ref_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAN','robot',2)`)
	})

	t.Run("account links require same Telegram person and active merchant owner", func(t *testing.T) {
		if _, err := admin.Exec(ctx, `INSERT INTO iam.person (public_id,tg_user_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAT',2002)`); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO iam.account (public_id,person_id,role) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAV',id,'buyer' FROM iam.person WHERE tg_user_id=2002`); err != nil {
			t.Fatal(err)
		}
		assertSQLState(t, ctx, admin, "23514", `INSERT INTO iam.account_link (public_id,buyer_account_id,merchant_account_id) SELECT '01ARZ3NDEKTSV4RRFFQ69G5FAW',b.id,m.id FROM iam.account b,iam.account m WHERE b.public_id='01ARZ3NDEKTSV4RRFFQ69G5FAV' AND m.public_id='`+ownerPublicID+`'`)
	})

	t.Run("account link service links and unlinks with one distinct audit row per action", func(t *testing.T) {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		service := iam.NewAccountLinkService(iam.PostgresAccountLinkRepository{DB: pool}, ids.NewULID)

		link, err := service.Link(ctx, ownerPublicID, buyerPublicID)
		if err != nil {
			t.Fatalf("first link: %v", err)
		}
		if _, err := service.Link(ctx, ownerPublicID, buyerPublicID); !errors.Is(err, iam.ErrAccountLinkExists) {
			t.Fatalf("duplicate active link error = %v, want ErrAccountLinkExists", err)
		}
		// A buyer belonging to a different Telegram person is not a candidate.
		if _, err := service.Link(ctx, ownerPublicID, "01ARZ3NDEKTSV4RRFFQ69G5FAV"); !errors.Is(err, iam.ErrAccountLinkForbidden) {
			t.Fatalf("other person's buyer error = %v, want ErrAccountLinkForbidden", err)
		}
		if err := service.Unlink(ctx, ownerPublicID, link.PublicID); err != nil {
			t.Fatalf("unlink: %v", err)
		}
		if err := service.Unlink(ctx, ownerPublicID, link.PublicID); !errors.Is(err, iam.ErrAccountLinkNotFound) {
			t.Fatalf("second unlink error = %v, want ErrAccountLinkNotFound", err)
		}
		// Link and unlink again: the unlink entry used to reuse the link's identifier as the log row's
		// public_id, which violates the unique constraint on the second cycle.
		again, err := service.Link(ctx, ownerPublicID, buyerPublicID)
		if err != nil {
			t.Fatalf("relink: %v", err)
		}
		if err := service.Unlink(ctx, ownerPublicID, again.PublicID); err != nil {
			t.Fatalf("second unlink cycle: %v", err)
		}
		var rows, distinct int
		if err := admin.QueryRow(ctx, `SELECT count(*), count(DISTINCT public_id) FROM iam.merchant_op_log WHERE action IN ('account_linked','account_unlinked')`).Scan(&rows, &distinct); err != nil {
			t.Fatal(err)
		}
		if rows != 4 || distinct != 4 {
			t.Fatalf("audit rows=%d distinct ids=%d, want 4 and 4", rows, distinct)
		}
	})

	t.Run("permission seeds match approved matrices", func(t *testing.T) {
		assertAdminMatrix(t, ctx, admin)
		assertMerchantTemplate(t, ctx, admin)
		seedSQL := migrationSeedSQL(t)
		if _, err := admin.Exec(ctx, seedSQL); err != nil {
			t.Fatalf("repeat seed: %v", err)
		}
		assertAdminMatrix(t, ctx, admin)
		assertMerchantTemplate(t, ctx, admin)
	})

	t.Run("database roles enforce module and sensitive column boundaries", func(t *testing.T) {
		iamLogin := createLogin(t, ctx, admin, dsn, "a01_test_iam", "iam_rw")
		defer iamLogin.Close(ctx)
		if _, err := iamLogin.Exec(ctx, `SELECT public_id FROM iam.account LIMIT 1`); err != nil {
			t.Fatalf("iam read: %v", err)
		}
		if _, err := iamLogin.Exec(ctx, `INSERT INTO iam.actor (public_id,kind,ref_id) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FAP','external',99)`); err != nil {
			t.Fatalf("iam write: %v", err)
		}
		ledgerLogin := createLogin(t, ctx, admin, dsn, "a01_test_ledger", "ledger_rw")
		defer ledgerLogin.Close(ctx)
		assertSQLState(t, ctx, ledgerLogin, "42501", `SELECT * FROM iam.account`)

		reportLogin := createLogin(t, ctx, admin, dsn, "a01_test_report", "report_ro")
		defer reportLogin.Close(ctx)
		if _, err := reportLogin.Exec(ctx, `SELECT public_id,username,token_last4 FROM iam.bot`); err != nil {
			t.Fatalf("report safe bot columns: %v", err)
		}
		for _, statement := range []string{
			`SELECT token_enc FROM iam.bot`,
			`SELECT webhook_secret_enc FROM iam.bot`,
			`SELECT password_hash FROM iam.admin_user`,
			`SELECT totp_secret_enc FROM iam.admin_user`,
			`SELECT refresh_token_hash FROM iam.session`,
			`SELECT previous_hash FROM iam.session`,
			`SELECT phone_enc FROM iam.person`,
		} {
			assertSQLState(t, ctx, reportLogin, "42501", statement)
		}
		assertSQLState(t, ctx, reportLogin, "42501", `UPDATE iam.account SET status='disabled'`)
	})
}

func seedFixtures(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO iam.person (public_id,tg_user_id) VALUES ($1,1001)`, personPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO iam.merchant (public_id,name) VALUES ($1,'{"zh":"M","en":"M","km":"M"}')`, merchantPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO iam.account (public_id,person_id,role) SELECT $1,id,'buyer' FROM iam.person WHERE public_id=$2`, buyerPublicID, personPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO iam.account (public_id,person_id,role,merchant_id) SELECT $1,p.id,'merchant_owner',m.id FROM iam.person p,iam.merchant m WHERE p.public_id=$2 AND m.public_id=$3`, ownerPublicID, personPublicID, merchantPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE iam.merchant SET owner_account_id = a.id FROM iam.account a WHERE iam.merchant.public_id=$1 AND a.public_id=$2`, merchantPublicID, ownerPublicID); err != nil {
		t.Fatal(err)
	}
}

func insertBot(t *testing.T, ctx context.Context, conn *pgx.Conn, publicID, end, username, poolRole, status string, priority int) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO iam.bot (public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES ($1,$2,$3,$4,$5,'\x01','\x02',$6)`, publicID, end, username, poolRole, status, priority); err != nil {
		t.Fatal(err)
	}
}

func insertAccountBot(t *testing.T, ctx context.Context, conn *pgx.Conn, publicID string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO iam.account_bot (public_id,account_id,bot_id) SELECT $1,a.id,b.id FROM iam.account a,iam.bot b WHERE a.public_id=$2 AND b.username='buyer_primary'`, publicID, buyerPublicID); err != nil {
		t.Fatal(err)
	}
}

func insertStaff(t *testing.T, ctx context.Context, conn *pgx.Conn, publicID, inviteCode, phoneHash string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO iam.merchant_staff (public_id,merchant_id,name,phone_enc,phone_hash,role,status,invite_code) SELECT $1,id,'A','\x01',$2::bytea,'manager','invited',$3 FROM iam.merchant WHERE public_id=$4`, publicID, phoneHash, inviteCode, merchantPublicID); err != nil {
		t.Fatal(err)
	}
}

func insertAccountLink(t *testing.T, ctx context.Context, conn *pgx.Conn, publicID string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO iam.account_link (public_id,buyer_account_id,merchant_account_id) SELECT $1,b.id,m.id FROM iam.account b,iam.account m WHERE b.public_id=$2 AND m.public_id=$3`, publicID, buyerPublicID, ownerPublicID); err != nil {
		t.Fatal(err)
	}
}

func assertAdminMatrix(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	modules := []string{"dash", "usr", "mer", "prod", "ord", "fund", "wd", "mod", "logi", "integ", "rbac", "sens", "cfg", "fin", "aud", "ops", "disp"}
	roles := []string{"super", "ops", "finance", "logi", "cs", "auditor"}
	rows := []string{
		"evvvvv", "eevnvv", "eevvvv", "eennvv", "evvvvv", "enennv", "enennv", "eennnv", "evnenv",
		"ennvnv", "ennnnv", "envnvn", "eennnv", "enennv", "envnne", "eevvvv", "eevnnv",
	}
	for i, module := range modules {
		for j, role := range roles {
			want := map[byte]string{'e': "edit", 'v': "view", 'n': "none"}[rows[i][j]]
			var got string
			if err := conn.QueryRow(ctx, `SELECT level FROM iam.admin_role_perm WHERE role=$1 AND module=$2`, role, module).Scan(&got); err != nil {
				t.Fatalf("%s/%s: %v", role, module, err)
			}
			if got != want {
				t.Errorf("%s/%s=%s, want %s", role, module, got, want)
			}
		}
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM iam.admin_role_perm`).Scan(&count); err != nil || count != 102 {
		t.Fatalf("admin matrix count=%d err=%v", count, err)
	}
}

func assertMerchantTemplate(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	modules := []string{"dash", "prod", "orders", "store", "fin", "ads", "prof", "staff", "ai"}
	wantByRole := map[string]string{"manager": "veeeveeve", "sales": "veevnnvne", "warehouse": "vvvennvne", "finance": "vnvvevvne"}
	for role, levels := range wantByRole {
		for i, module := range modules {
			want := map[byte]string{'e': "edit", 'v': "view", 'n': "none"}[levels[i]]
			var got string
			if err := conn.QueryRow(ctx, `SELECT level FROM iam.merchant_role_perm_template WHERE role=$1 AND module=$2`, role, module).Scan(&got); err != nil {
				t.Fatalf("%s/%s: %v", role, module, err)
			}
			if got != want {
				t.Errorf("%s/%s=%s, want %s", role, module, got, want)
			}
		}
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM iam.merchant_role_perm_template`).Scan(&count); err != nil || count != 36 {
		t.Fatalf("merchant template count=%d err=%v", count, err)
	}
}

func migrationSeedSQL(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(migrationDirectory(t), "00003_iam_tables.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	start := strings.Index(text, "-- seed:start")
	end := strings.Index(text, "-- seed:end")
	if start < 0 || end <= start {
		t.Fatal("seed markers not found")
	}
	return text[start+len("-- seed:start") : end]
}

func createLogin(t *testing.T, ctx context.Context, admin *pgx.Conn, dsn, login, permission string) *pgx.Conn {
	t.Helper()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD 'test-only'`, pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`GRANT %s TO %s`, pgx.Identifier{permission}.Sanitize(), pgx.Identifier{login}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(login, "test-only")
	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func assertSQLState(t *testing.T, ctx context.Context, conn *pgx.Conn, want, statement string) {
	t.Helper()
	_, err := conn.Exec(ctx, statement)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("statement %q error=%v, want SQLSTATE %s", statement, err, want)
	}
}

func assertIAMTableCount(t *testing.T, database *sql.DB, want int) {
	t.Helper()
	var got int
	if err := database.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='iam' AND table_type='BASE TABLE'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("iam table count=%d, want %d", got, want)
	}
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate migration directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}
