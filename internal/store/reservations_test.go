package store

import "testing"

func TestReservationDiscountCapsFixedDiscountAtSubtotal(t *testing.T) {
	coupon := &Coupon{DiscountType: "amount", AmountOffCents: 2500}
	if got := reservationDiscount(coupon, 1000); got != 1000 {
		t.Fatalf("expected discount to cap at subtotal, got %d", got)
	}
}

func TestReservationDiscountCalculatesPercent(t *testing.T) {
	coupon := &Coupon{DiscountType: "percent", PercentOff: 25}
	if got := reservationDiscount(coupon, 4000); got != 1000 {
		t.Fatalf("expected quarter discount of 1000, got %d", got)
	}
}
