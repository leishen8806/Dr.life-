package logistics

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/logistics/routefit"
)

var ErrSKUMappingMissing = errors.New("logistics: SKU_MAPPING_MISSING")

type FulfillmentLine struct {
	SKU      string
	Quantity int64
}

type FulfillmentOrder struct {
	MerchantOrderPublicID string
	ExternalOrderID       string
	OwnerRef              string
	Lines                 []FulfillmentLine
}

func EnqueuePaidFulfillmentTx(ctx context.Context, tx pgx.Tx, order FulfillmentOrder, serviceLevel string) (bool, error) {
	command, err := BuildFulfillmentCommandTx(ctx, tx, order, serviceLevel)
	if errors.Is(err, ErrSKUMappingMissing) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, EnqueueFulfillmentTx(ctx, tx, command)
}

// BuildFulfillmentCommandTx consumes trade-owned order data as values and
// reads only the logistics-owned SKU map in the caller's transaction.
func BuildFulfillmentCommandTx(ctx context.Context, tx pgx.Tx, order FulfillmentOrder, serviceLevel string) (routefit.CreateShipmentCmd, error) {
	if tx == nil || strings.TrimSpace(order.MerchantOrderPublicID) == "" {
		return routefit.CreateShipmentCmd{}, errors.New("logistics: fulfillment transaction and order are required")
	}
	if strings.TrimSpace(serviceLevel) == "" {
		serviceLevel = "S2"
	}
	if strings.TrimSpace(order.ExternalOrderID) == "" || strings.TrimSpace(order.OwnerRef) == "" {
		return routefit.CreateShipmentCmd{}, fmt.Errorf("%w: merchant public_id", ErrSKUMappingMissing)
	}
	var lines []routefit.Line
	warehouseIDForOrder := ""
	for _, line := range order.Lines {
		var skuID, warehouseID string
		var unitRatio float64
		err := tx.QueryRow(ctx, `SELECT COALESCE(routefit_sku_id,''), COALESCE(warehouse_id,''), COALESCE(unit_ratio,0) FROM logistics.sku_map WHERE sku_public_id=$1 AND state='active'`, line.SKU).Scan(&skuID, &warehouseID, &unitRatio)
		if errors.Is(err, pgx.ErrNoRows) {
			return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
		}
		if err != nil {
			return routefit.CreateShipmentCmd{}, err
		}
		if skuID == "" || warehouseID == "" || unitRatio <= 0 || line.Quantity <= 0 {
			return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
		}
		if warehouseIDForOrder == "" {
			warehouseIDForOrder = warehouseID
		} else if warehouseIDForOrder != warehouseID {
			return routefit.CreateShipmentCmd{}, fmt.Errorf("%w: multiple warehouses", ErrSKUMappingMissing)
		}
		lines = append(lines, routefit.Line{Ref: line.SKU, SKU: skuID, Quantity: int64(float64(line.Quantity) * unitRatio)})
	}
	if len(lines) == 0 {
		return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
	}
	return routefit.CreateShipmentCmd{ExternalOrderID: order.ExternalOrderID, OwnerRef: order.OwnerRef, WarehouseID: warehouseIDForOrder, Lines: lines, ServiceLevel: serviceLevel}, nil
}
