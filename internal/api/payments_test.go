package api

import "testing"

func TestSifaloAmountCloseEnoughAcceptsNetFeeShortfall(t *testing.T) {
	if !sifaloAmountCloseEnough(187, 190) {
		t.Fatal("expected Sifalo net-after-fee amount to be accepted")
	}
	if !sifaloAmountCloseEnough(985, 1000) {
		t.Fatal("expected percentage-sized Sifalo net-after-fee amount to be accepted")
	}
}

func TestSifaloAmountCloseEnoughRejectsLargeShortfall(t *testing.T) {
	if sifaloAmountCloseEnough(100, 190) {
		t.Fatal("expected large Sifalo amount shortfall to require review")
	}
	if sifaloAmountCloseEnough(0, 190) {
		t.Fatal("expected zero Sifalo amount to require review")
	}
}

func TestSifaloVerifyStatusHelpers(t *testing.T) {
	if !sifaloVerifySuccess(&sifaloVerifyResp{Code: 601}) {
		t.Fatal("expected code 601 to be successful")
	}
	if !sifaloVerifyFailure(&sifaloVerifyResp{Code: 604}) {
		t.Fatal("expected code 604 to be failed")
	}
}
