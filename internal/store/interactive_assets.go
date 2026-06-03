package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const BrandClarityWorksheetSlug = "brand-clarity-worksheet"
const defaultInteractiveAssetUses = 5

type InteractiveAssetEntitlement struct {
	ID            int64      `json:"id" db:"id"`
	UserID        int64      `json:"userId" db:"user_id"`
	OrderID       int64      `json:"orderId" db:"order_id"`
	ProductID     *int64     `json:"productId" db:"product_id"`
	AssetSlug     string     `json:"assetSlug" db:"asset_slug"`
	AssetName     string     `json:"assetName" db:"asset_name"`
	LicenseID     string     `json:"licenseId" db:"license_id"`
	UsesRemaining int        `json:"usesRemaining" db:"uses_remaining"`
	ExpiresAt     *time.Time `json:"expiresAt" db:"expires_at"`
	Status        string     `json:"status" db:"status"`
	CreatedAt     time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt     time.Time  `json:"updatedAt" db:"updated_at"`
}

type InteractiveAssetEvent struct {
	EntitlementID int64
	UserID        int64
	AssetSlug     string
	EventType     string
	IPAddress     string
	UserAgent     string
}

func interactiveAssetName(slug string) string {
	switch slug {
	case BrandClarityWorksheetSlug:
		return "Brand Clarity Worksheet"
	default:
		return slug
	}
}

func newLicenseID(assetSlug string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	prefix := "KKK"
	for _, part := range strings.Split(assetSlug, "-") {
		if part != "" {
			prefix += strings.ToUpper(part[:1])
		}
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

// GrantInteractiveAssetsForOrder creates entitlement rows for any products on
// a confirmed/fulfilled shop order that map to an interactive asset.
func (s *Store) GrantInteractiveAssetsForOrder(ctx context.Context, order *Order) error {
	if order == nil || order.UserID == nil || order.Kind != "shop" {
		return nil
	}
	if order.Status != "confirmed" && order.Status != "fulfilled" {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, item := range order.Items {
		if item.ProductID == nil {
			continue
		}
		var assetSlug string
		err := tx.QueryRow(ctx,
			`SELECT interactive_asset_slug FROM products WHERE id = $1`,
			*item.ProductID,
		).Scan(&assetSlug)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return err
		}
		if strings.TrimSpace(assetSlug) == "" {
			continue
		}
		uses := defaultInteractiveAssetUses
		if item.Quantity > 1 {
			uses *= item.Quantity
		}
		licenseID, err := newLicenseID(assetSlug)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO interactive_asset_entitlements
			    (user_id, order_id, product_id, asset_slug, license_id, uses_remaining, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'active')
			ON CONFLICT (user_id, order_id, asset_slug) DO NOTHING`,
			*order.UserID, order.ID, item.ProductID, assetSlug, licenseID, uses,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListInteractiveAssetEntitlements(ctx context.Context, userID int64) ([]InteractiveAssetEntitlement, error) {
	items, err := queryRows[InteractiveAssetEntitlement](ctx, s.pool, `
		SELECT e.id, e.user_id, e.order_id, e.product_id, e.asset_slug,
		       COALESCE(NULLIF(p.name, ''), e.asset_slug) AS asset_name,
		       e.license_id, e.uses_remaining, e.expires_at, e.status, e.created_at, e.updated_at
		FROM interactive_asset_entitlements e
		LEFT JOIN products p ON p.id = e.product_id
		WHERE e.user_id = $1
		  AND e.status = 'active'
		  AND (e.expires_at IS NULL OR e.expires_at > now())
		ORDER BY e.created_at DESC, e.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].AssetName == "" || items[i].AssetName == items[i].AssetSlug {
			items[i].AssetName = interactiveAssetName(items[i].AssetSlug)
		}
	}
	return items, nil
}

func (s *Store) GetInteractiveAssetEntitlement(ctx context.Context, userID int64, assetSlug string) (*InteractiveAssetEntitlement, error) {
	item, err := queryOne[InteractiveAssetEntitlement](ctx, s.pool, `
		SELECT e.id, e.user_id, e.order_id, e.product_id, e.asset_slug,
		       COALESCE(NULLIF(p.name, ''), e.asset_slug) AS asset_name,
		       e.license_id, e.uses_remaining, e.expires_at, e.status, e.created_at, e.updated_at
		FROM interactive_asset_entitlements e
		LEFT JOIN products p ON p.id = e.product_id
		WHERE e.user_id = $1
		  AND e.asset_slug = $2
		  AND e.status = 'active'
		  AND (e.expires_at IS NULL OR e.expires_at > now())
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT 1`, userID, assetSlug)
	if err != nil {
		return nil, err
	}
	if item.AssetName == "" || item.AssetName == item.AssetSlug {
		item.AssetName = interactiveAssetName(item.AssetSlug)
	}
	return item, nil
}

func (s *Store) ConsumeInteractiveAssetExport(ctx context.Context, userID int64, assetSlug string) (*InteractiveAssetEntitlement, error) {
	var item InteractiveAssetEntitlement
	err := s.pool.QueryRow(ctx, `
		UPDATE interactive_asset_entitlements e
		SET uses_remaining = uses_remaining - 1, updated_at = now()
		WHERE e.id = (
			SELECT id FROM interactive_asset_entitlements
			WHERE user_id = $1
			  AND asset_slug = $2
			  AND status = 'active'
			  AND uses_remaining > 0
			  AND (expires_at IS NULL OR expires_at > now())
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		)
		RETURNING e.id, e.user_id, e.order_id, e.product_id, e.asset_slug,
		          e.asset_slug AS asset_name, e.license_id, e.uses_remaining,
		          e.expires_at, e.status, e.created_at, e.updated_at`,
		userID, assetSlug,
	).Scan(
		&item.ID, &item.UserID, &item.OrderID, &item.ProductID, &item.AssetSlug,
		&item.AssetName, &item.LicenseID, &item.UsesRemaining, &item.ExpiresAt,
		&item.Status, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			if _, lookupErr := s.GetInteractiveAssetEntitlement(ctx, userID, assetSlug); lookupErr == nil {
				return nil, ErrAssetUseLimit
			}
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.AssetName = interactiveAssetName(item.AssetSlug)
	return &item, nil
}

func (s *Store) LogInteractiveAssetEvent(ctx context.Context, e InteractiveAssetEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO interactive_asset_events
		    (entitlement_id, user_id, asset_slug, event_type, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.EntitlementID, e.UserID, e.AssetSlug, e.EventType, e.IPAddress, e.UserAgent,
	)
	return err
}
