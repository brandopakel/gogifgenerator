package account

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestCatalogEnforcesSignupQualityAndPaidFeatures(t *testing.T) {
	catalog := NewCatalog(CatalogOptions{})
	guest := Principal{ID: "guest_1", PlanID: PlanGuest}
	if _, err := catalog.Quote(guest, Operation{Kind: "gif", Mode: "semantic", Width: 320, Height: 320, Frames: 8}); !errors.Is(err, ErrSignInRequired) {
		t.Fatalf("guest semantic error = %v", err)
	}
	free := Principal{ID: "usr_1", UserID: "usr_1", PlanID: PlanFree, Authenticated: true}
	if _, err := catalog.Quote(free, Operation{Kind: "model"}); !errors.Is(err, ErrUpgradeRequired) {
		t.Fatalf("free model error = %v", err)
	}
	if _, err := catalog.Quote(free, Operation{Kind: "gif", Mode: "semantic", Width: 720, Height: 720, Frames: 24}); !errors.Is(err, ErrQualityLimit) {
		t.Fatalf("free quality error = %v", err)
	}
	creator := Principal{ID: "usr_2", UserID: "usr_2", PlanID: PlanCreator, Authenticated: true}
	quote, err := catalog.Quote(creator, Operation{Kind: "model"})
	if err != nil || quote.Cost != 50 {
		t.Fatalf("creator model quote = %#v, %v", quote, err)
	}
}

func TestRepositoryUpsertsVerifiedIdentityAndBilling(t *testing.T) {
	repository := NewRepository(store.NewMemoryKV())
	repository.now = func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) }
	identity := Identity{Issuer: "https://issuer.example", Subject: "subject-1", Email: "Person@Example.com", EmailVerified: true, Name: "Person"}
	user, err := repository.UpsertIdentity(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if user.PlanID != PlanFree || user.Email != "person@example.com" {
		t.Fatalf("user = %#v", user)
	}
	again, err := repository.UpsertIdentity(context.Background(), identity)
	if err != nil || again.ID != user.ID {
		t.Fatalf("second upsert = %#v, %v", again, err)
	}
	paid, err := repository.UpdateBilling(context.Background(), user.ID, BillingUpdate{
		PlanID: PlanCreator, StripeCustomerID: "cus_123", StripeSubscriptionID: "sub_123", SubscriptionStatus: "active",
	})
	if err != nil || paid.PlanID != PlanCreator {
		t.Fatalf("paid user = %#v, %v", paid, err)
	}
	byCustomer, err := repository.FindByStripeCustomer(context.Background(), "cus_123")
	if err != nil || byCustomer.ID != user.ID {
		t.Fatalf("FindByStripeCustomer = %#v, %v", byCustomer, err)
	}
	downgraded, err := repository.UpdateBilling(context.Background(), user.ID, BillingUpdate{SubscriptionStatus: "canceled"})
	if err != nil || downgraded.PlanID != PlanFree {
		t.Fatalf("downgraded = %#v, %v", downgraded, err)
	}
}

func TestLedgerReservesCompletesAndReleasesCredits(t *testing.T) {
	ledger := NewLedger(store.NewMemoryKV())
	ledger.now = func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) }
	plan := Plan{Credits: 10, CreditPeriod: "month"}
	reservation, _, err := ledger.Reserve(context.Background(), "usr_1", plan, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.Reserve(context.Background(), "usr_1", plan, 3); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("second reservation error = %v", err)
	}
	if err := ledger.Release(context.Background(), "usr_1", plan, reservation.ID); err != nil {
		t.Fatal(err)
	}
	reservation, _, err = ledger.Reserve(context.Background(), "usr_1", plan, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Complete(context.Background(), "usr_1", plan, reservation.ID); err != nil {
		t.Fatal(err)
	}
	usage, err := ledger.Summary(context.Background(), "usr_1", plan)
	if err != nil || usage.Used != 8 {
		t.Fatalf("usage = %#v, %v", usage, err)
	}
}
