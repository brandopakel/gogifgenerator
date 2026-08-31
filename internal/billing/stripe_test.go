package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

func TestStripeCreatesHostedCheckoutAndPortal(t *testing.T) {
	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if username, _, ok := r.BasicAuth(); !ok || username != "sk_test" {
			t.Fatal("missing Stripe authorization")
		}
		data, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(data))
		requests = append(requests, values)
		if strings.Contains(r.URL.Path, "checkout") {
			_, _ = w.Write([]byte(`{"url":"https://checkout.stripe.test/session"}`))
		} else {
			_, _ = w.Write([]byte(`{"url":"https://billing.stripe.test/portal"}`))
		}
	}))
	defer server.Close()
	catalog := account.NewCatalog(account.CatalogOptions{CreatorPriceID: "price_creator"})
	stripe, err := NewStripe(Options{
		SecretKey: "sk_test", WebhookSecret: "whsec_test", PublicURL: "https://app.example", APIBaseURL: server.URL,
		Catalog: catalog, Accounts: account.NewRepository(store.NewMemoryKV()), KV: store.NewMemoryKV(),
	})
	if err != nil {
		t.Fatal(err)
	}
	user := account.User{ID: "usr_1", Email: "person@example.com"}
	checkout, err := stripe.CreateCheckout(context.Background(), user, account.PlanCreator)
	if err != nil || checkout == "" || requests[0].Get("subscription_data[metadata][user_id]") != user.ID {
		t.Fatalf("checkout = %q, %#v, %v", checkout, requests, err)
	}
	user.StripeCustomerID = "cus_1"
	portal, err := stripe.CreatePortal(context.Background(), user)
	if err != nil || portal == "" || requests[1].Get("customer") != "cus_1" {
		t.Fatalf("portal = %q, %#v, %v", portal, requests, err)
	}
}

func TestStripeWebhookSynchronizesAndDeduplicatesSubscription(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	kv := store.NewMemoryKV()
	accounts := account.NewRepository(kv)
	user, err := accounts.UpsertIdentity(context.Background(), account.Identity{Issuer: "issuer", Subject: "subject", Email: "person@example.com", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	catalog := account.NewCatalog(account.CatalogOptions{CreatorPriceID: "price_creator"})
	stripe, err := NewStripe(Options{SecretKey: "sk", WebhookSecret: "whsec", PublicURL: "https://app.example", Catalog: catalog, Accounts: accounts, KV: kv, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(fmt.Sprintf(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"customer":"cus_1","subscription":"sub_1","client_reference_id":%q,"metadata":{"user_id":%q,"plan_id":"creator"}}}}`, user.ID, user.ID))
	signature := sign(payload, "whsec", now.Unix())
	if err := stripe.HandleWebhook(context.Background(), payload, signature); err != nil {
		t.Fatal(err)
	}
	if err := stripe.HandleWebhook(context.Background(), payload, signature); err != nil {
		t.Fatalf("duplicate event = %v", err)
	}
	updated, err := accounts.Get(context.Background(), user.ID)
	if err != nil || updated.PlanID != account.PlanCreator || updated.StripeCustomerID != "cus_1" {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	if err := stripe.HandleWebhook(context.Background(), payload, "t=1,v1=bad"); err == nil {
		t.Fatal("invalid signature accepted")
	}
}

func sign(payload []byte, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(payload)
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}
