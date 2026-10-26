package ledger_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

const publicAPIGuardMessage = "新增对外账务方法必须经过评审并更新白名单"

var publicServiceMethods = []string{
	"AdCharge", "Adjust", "AdRefund", "AdSpend", "ApproveReview", "BankDiff",
	"CancelWithdrawal", "CarrierPay", "ClaimOutbox", "ConfirmPayout", "DecryptWithdrawalPayee",
	"DeliveryHold", "DeliveryRefund", "DeliveryRelease", "DisputeHold", "EnsureWallet",
	"FailWithdrawal", "FreezeHold", "GetByID", "GetByIdemKey", "GetHold", "GetReconciliationRun",
	"GetReversalOf", "GetWithdrawal", "ListHoldsByAccount", "ListLargePending",
	"ListOverdueWithdrawals", "ListPayoutFreezes", "MemberFee", "OldestUnsentOutboxAge", "RefundHold",
	"ReleasePayoutFreeze", "RequestWithdrawal", "ResolveDispute", "Reverse",
	"RunDailyReconciliation", "SettleHold", "SnapshotDaily", "StartReview", "StorageFee",
	"StopSpotChecks",
	"Sweep", "Topup", "VerifyHoldsMatchFrozen", "VerifyPendingPayoutMatchesWithdrawals",
}

func TestPublicAPISurface(t *testing.T) {
	typeOfService := reflect.TypeOf((*ledger.Service)(nil))
	got := make([]string, 0, typeOfService.NumMethod())
	for i := 0; i < typeOfService.NumMethod(); i++ {
		got = append(got, typeOfService.Method(i).Name)
	}
	if err := comparePublicMethods(got, publicServiceMethods); err != nil {
		t.Fatal(err)
	}

	t.Run("negative proof rejects an extra method", func(t *testing.T) {
		fake := append(append([]string(nil), publicServiceMethods...), "UnsafePost")
		if err := comparePublicMethods(fake, publicServiceMethods); err == nil || !strings.Contains(err.Error(), publicAPIGuardMessage) {
			t.Fatalf("guard accepted an extra exported method: %v", err)
		}
	})
}

func comparePublicMethods(got, want []string) error {
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if reflect.DeepEqual(got, want) {
		return nil
	}
	return fmt.Errorf("%s: got %v, want %v", publicAPIGuardMessage, got, want)
}

type ledgerImport struct {
	packagePath string
	importPath  string
}

func TestLedgerImportGuard(t *testing.T) {
	imports, err := scanLedgerImports(backendRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"cmd/ledgerctl": true, "internal/platform/queue/outbox": true}
	if err := validateLedgerImports(imports, allowed); err != nil {
		t.Fatal(err)
	}

	t.Run("negative proof rejects an unreviewed importer", func(t *testing.T) {
		fake := []ledgerImport{{packagePath: "internal/trade", importPath: "github.com/leishen8806/Dr.life-/backend/internal/ledger"}}
		if err := validateLedgerImports(fake, allowed); err == nil {
			t.Fatal("import guard accepted an unreviewed ledger importer")
		}
	})

	t.Run("negative proof rejects implementation imports", func(t *testing.T) {
		fake := []ledgerImport{{packagePath: "cmd/ledgerctl", importPath: "github.com/leishen8806/Dr.life-/backend/internal/ledger/internal/posting"}}
		if err := validateLedgerImports(fake, allowed); err == nil {
			t.Fatal("import guard accepted a ledger implementation import")
		}
	})
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func scanLedgerImports(root string) ([]ledgerImport, error) {
	const ledgerPath = "github.com/leishen8806/Dr.life-/backend/internal/ledger"
	var result []ledgerImport
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == filepath.Join(root, "internal", "ledger") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		packagePath := filepath.ToSlash(relDir)
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath == ledgerPath || strings.HasPrefix(importPath, ledgerPath+"/") {
				result = append(result, ledgerImport{packagePath: packagePath, importPath: importPath})
			}
		}
		return nil
	})
	return result, err
}

func validateLedgerImports(imports []ledgerImport, allowed map[string]bool) error {
	const ledgerPath = "github.com/leishen8806/Dr.life-/backend/internal/ledger"
	for _, item := range imports {
		if strings.HasPrefix(item.importPath, ledgerPath+"/") {
			return fmt.Errorf("%s imports ledger implementation %s", item.packagePath, item.importPath)
		}
		if !allowed[item.packagePath] {
			return fmt.Errorf("%s imports ledger without review", item.packagePath)
		}
	}
	return nil
}
