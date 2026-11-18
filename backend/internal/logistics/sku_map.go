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

func EnqueuePaidFulfillmentTx(ctx context.Context, tx pgx.Tx, merchantOrderPublicID, serviceLevel string) (bool, error) {
	command, err := BuildFulfillmentCommandTx(ctx, tx, merchantOrderPublicID, serviceLevel)
	if errors.Is(err, ErrSKUMappingMissing) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, EnqueueFulfillmentTx(ctx, tx, command)
}

// BuildFulfillmentCommandTx reads the trade order and the logistics-owned SKU
// map in the caller's transaction. It deliberately returns no RouteFit
// command when a map is absent or disabled; the caller can move the suborder
// to its exception path with SKU_MAPPING_MISSING.
func BuildFulfillmentCommandTx(ctx context.Context, tx pgx.Tx, merchantOrderPublicID, serviceLevel string) (routefit.CreateShipmentCmd, error) {
	merchantOrderPublicID = strings.TrimSpace(merchantOrderPublicID)
	if tx == nil || merchantOrderPublicID == "" {
		return routefit.CreateShipmentCmd{}, errors.New("logistics: fulfillment transaction and order are required")
	}
	if strings.TrimSpace(serviceLevel) == "" {
		serviceLevel = "S2"
	}
	var externalOrderID, ownerRef string
	if err := tx.QueryRow(ctx, `
		SELECT 'DRL-' || child.public_id, COALESCE(child.merchant_public_id, '')
		FROM trade.merchant_order child
		WHERE child.public_id = $1
		FOR SHARE`, merchantOrderPublicID).Scan(&externalOrderID, &ownerRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
		}
		return routefit.CreateShipmentCmd{}, err
	}
	if ownerRef == "" {
		return routefit.CreateShipmentCmd{}, fmt.Errorf("%w: merchant public_id", ErrSKUMappingMissing)
	}
	rows, err := tx.Query(ctx, `
		SELECT line.sku_public_id, COALESCE(map.routefit_sku_id, ''), COALESCE(map.warehouse_id, ''), COALESCE(map.unit_ratio, 0), line.quantity
		FROM trade.order_line line
		JOIN trade.merchant_order child ON child.id = line.merchant_order_id
		JOIN catalog.sku sku ON sku.public_id = line.sku_public_id
		LEFT JOIN logistics.sku_map map ON map.product_id = sku.product_id AND map.state = 'active'
		WHERE child.public_id = $1
		ORDER BY line.id`, merchantOrderPublicID)
	if err != nil {
		return routefit.CreateShipmentCmd{}, err
	}
	defer rows.Close()
	var lines []routefit.Line
	warehouseIDForOrder := ""
	for rows.Next() {
		var lineRef, skuID, warehouseID string
		var quantity int64
		var unitRatio float64
		if err := rows.Scan(&lineRef, &skuID, &warehouseID, &unitRatio, &quantity); err != nil {
			return routefit.CreateShipmentCmd{}, err
		}
		if skuID == "" || warehouseID == "" || unitRatio <= 0 || quantity <= 0 {
			return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
		}
		if warehouseIDForOrder == "" {
			warehouseIDForOrder = warehouseID
		} else if warehouseIDForOrder != warehouseID {
			return routefit.CreateShipmentCmd{}, fmt.Errorf("%w: multiple warehouses", ErrSKUMappingMissing)
		}
		lines = append(lines, routefit.Line{Ref: lineRef, SKU: skuID, Quantity: int64(float64(quantity) * unitRatio)})
	}
	if err := rows.Err(); err != nil {
		return routefit.CreateShipmentCmd{}, err
	}
	if len(lines) == 0 {
		return routefit.CreateShipmentCmd{}, ErrSKUMappingMissing
	}
	return routefit.CreateShipmentCmd{ExternalOrderID: externalOrderID, OwnerRef: ownerRef, WarehouseID: warehouseIDForOrder, Lines: lines, ServiceLevel: serviceLevel}, nil
}
