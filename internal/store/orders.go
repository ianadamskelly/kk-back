package store

import (
	"context"
	"time"
)

// Order is a customer purchase request. Kind splits revenue into buckets
// (shop / course / membership) so admin reports can break it down.
//
// Price breakdown: subtotal_cents = sum of line items at full price;
// discount_cents = coupon discount; credit_cents = store credit applied;
// total_cents = subtotal - discount - credit (what the customer actually
// owes the payment gateway).
type Order struct {
	ID             int64       `json:"id" db:"id"`
	UserID         *int64      `json:"userId" db:"user_id"`
	Kind           string      `json:"kind" db:"kind"`
	CustomerName   string      `json:"customerName" db:"customer_name"`
	CustomerEmail  string      `json:"customerEmail" db:"customer_email"`
	CustomerPhone  string      `json:"customerPhone" db:"customer_phone"`
	Note           string      `json:"note" db:"note"`
	SubtotalCents  int64       `json:"subtotalCents" db:"subtotal_cents"`
	DiscountCents  int64       `json:"discountCents" db:"discount_cents"`
	CreditCents    int64       `json:"creditCents" db:"credit_cents"`
	CouponID       *int64      `json:"couponId" db:"coupon_id"`
	CouponCode     string      `json:"couponCode" db:"coupon_code"`
	TotalCents     int64       `json:"totalCents" db:"total_cents"`
	Status         string      `json:"status" db:"status"`
	CreatedAt      time.Time   `json:"createdAt" db:"created_at"`
	Items          []OrderItem `json:"items" db:"-"`
}

// OrderItem is a single line on an order, with a price snapshot. Either
// ProductID or CourseID is set, depending on what was sold.
type OrderItem struct {
	ID             int64  `json:"id" db:"id"`
	OrderID        int64  `json:"orderId" db:"order_id"`
	ProductID      *int64 `json:"productId" db:"product_id"`
	CourseID       *int64 `json:"courseId" db:"course_id"`
	ProductName    string `json:"productName" db:"product_name"`
	UnitPriceCents int64  `json:"unitPriceCents" db:"unit_price_cents"`
	Quantity       int    `json:"quantity" db:"quantity"`
}

const orderSelect = `SELECT id, user_id, kind, customer_name, customer_email, customer_phone, note, subtotal_cents, discount_cents, credit_cents, coupon_id, coupon_code, total_cents, status, created_at FROM orders`
const orderItemSelect = `SELECT id, order_id, product_id, course_id, product_name, unit_price_cents, quantity FROM order_items`

// CreateOrder inserts an order and its line items in a single transaction.
func (s *Store) CreateOrder(ctx context.Context, o *Order, items []OrderItem) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	kind := o.Kind
	if kind == "" {
		kind = "shop"
	}
	// Fill any breakdown fields the caller forgot — keeps older code paths
	// that only set TotalCents working.
	if o.SubtotalCents == 0 {
		o.SubtotalCents = o.TotalCents + o.DiscountCents + o.CreditCents
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO orders (user_id, kind, customer_name, customer_email, customer_phone, note,
			subtotal_cents, discount_cents, credit_cents, coupon_id, coupon_code, total_cents, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'pending')
		RETURNING id, kind, status, created_at`,
		o.UserID, kind, o.CustomerName, o.CustomerEmail, o.CustomerPhone, o.Note,
		o.SubtotalCents, o.DiscountCents, o.CreditCents, o.CouponID, o.CouponCode, o.TotalCents,
	).Scan(&o.ID, &o.Kind, &o.Status, &o.CreatedAt); err != nil {
		return err
	}

	for i := range items {
		items[i].OrderID = o.ID
		if err := tx.QueryRow(ctx, `
			INSERT INTO order_items (order_id, product_id, course_id, product_name, unit_price_cents, quantity)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			items[i].OrderID, items[i].ProductID, items[i].CourseID, items[i].ProductName,
			items[i].UnitPriceCents, items[i].Quantity,
		).Scan(&items[i].ID); err != nil {
			return err
		}
	}
	o.Items = items
	return tx.Commit(ctx)
}

// ListOrders returns every order with its line items, newest first.
func (s *Store) ListOrders(ctx context.Context) ([]Order, error) {
	orders, err := queryRows[Order](ctx, s.pool,
		orderSelect+` ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	items, err := queryRows[OrderItem](ctx, s.pool, orderItemSelect)
	if err != nil {
		return nil, err
	}
	byOrder := map[int64][]OrderItem{}
	for _, it := range items {
		byOrder[it.OrderID] = append(byOrder[it.OrderID], it)
	}
	for i := range orders {
		orders[i].Items = byOrder[orders[i].ID]
		if orders[i].Items == nil {
			orders[i].Items = []OrderItem{}
		}
	}
	return orders, nil
}

// ListUserOrders returns orders placed by the given user, newest first.
func (s *Store) ListUserOrders(ctx context.Context, userID int64) ([]Order, error) {
	orders, err := queryRows[Order](ctx, s.pool,
		orderSelect+` WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return orders, nil
	}
	ids := make([]int64, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	items, err := queryRows[OrderItem](ctx, s.pool,
		orderItemSelect+` WHERE order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	byOrder := map[int64][]OrderItem{}
	for _, it := range items {
		byOrder[it.OrderID] = append(byOrder[it.OrderID], it)
	}
	for i := range orders {
		orders[i].Items = byOrder[orders[i].ID]
		if orders[i].Items == nil {
			orders[i].Items = []OrderItem{}
		}
	}
	return orders, nil
}

// GetOrder returns a single order with its line items.
func (s *Store) GetOrder(ctx context.Context, id int64) (*Order, error) {
	order, err := queryOne[Order](ctx, s.pool, orderSelect+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	items, err := queryRows[OrderItem](ctx, s.pool,
		orderItemSelect+` WHERE order_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	order.Items = items
	return order, nil
}

// UpdateOrderStatus changes the status of an order.
func (s *Store) UpdateOrderStatus(ctx context.Context, id int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE orders SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOrder removes an order and its line items.
func (s *Store) DeleteOrder(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
