// Package billing integrates GoGIF plans with Stripe-hosted Checkout and Portal.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/account"
	"github.com/brandopakel/gogifgenerator/internal/store"
)

const maxWebhookBytes = 1 << 20

type Options struct {
	SecretKey     string
	WebhookSecret string
	PublicURL     string
	APIBaseURL    string
	Catalog       account.Catalog
	Accounts      *account.Repository
	KV            store.KV
	HTTPClient    *http.Client
	Now           func() time.Time
}

type Stripe struct {
	secretKey     string
	webhookSecret string
	publicURL     string
	apiBaseURL    string
	catalog       account.Catalog
	accounts      *account.Repository
	kv            store.KV
	httpClient    *http.Client
	now           func() time.Time
	mu            sync.Mutex
}

func NewStripe(options Options) (*Stripe, error) {
	if strings.TrimSpace(options.SecretKey) == "" || strings.TrimSpace(options.WebhookSecret) == "" {
		return nil, errors.New("Stripe secret key and webhook secret are required")
	}
	parsed, err := url.Parse(options.PublicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("Stripe billing requires an HTTPS GOGIF_PUBLIC_URL")
	}
	if options.Accounts == nil || options.KV == nil {
		return nil, errors.New("Stripe billing requires account and idempotency stores")
	}
	apiBaseURL := strings.TrimRight(strings.TrimSpace(options.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.stripe.com/v1"
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Stripe{
		secretKey: options.SecretKey, webhookSecret: options.WebhookSecret, publicURL: strings.TrimRight(options.PublicURL, "/"),
		apiBaseURL: apiBaseURL, catalog: options.Catalog, accounts: options.Accounts, kv: options.KV, httpClient: client, now: now,
	}, nil
}

func (s *Stripe) CreateCheckout(ctx context.Context, user account.User, planID string) (string, error) {
	plan, ok := s.catalog.Get(strings.ToLower(strings.TrimSpace(planID)))
	if !ok || !plan.Paid || plan.StripePriceID == "" {
		return "", errors.New("the selected paid plan is not configured")
	}
	form := url.Values{
		"mode":                                 {"subscription"},
		"line_items[0][price]":                 {plan.StripePriceID},
		"line_items[0][quantity]":              {"1"},
		"success_url":                          {s.publicURL + "/?billing=success&session_id={CHECKOUT_SESSION_ID}"},
		"cancel_url":                           {s.publicURL + "/?billing=canceled"},
		"client_reference_id":                  {user.ID},
		"metadata[user_id]":                    {user.ID},
		"metadata[plan_id]":                    {plan.ID},
		"subscription_data[metadata][user_id]": {user.ID},
		"subscription_data[metadata][plan_id]": {plan.ID},
		"allow_promotion_codes":                {"true"},
	}
	if user.StripeCustomerID != "" {
		form.Set("customer", user.StripeCustomerID)
	} else {
		form.Set("customer_email", user.Email)
	}
	var response struct {
		URL string `json:"url"`
	}
	if err := s.postForm(ctx, "/checkout/sessions", form, &response); err != nil {
		return "", err
	}
	if response.URL == "" {
		return "", errors.New("Stripe Checkout did not return a URL")
	}
	return response.URL, nil
}

func (s *Stripe) CreatePortal(ctx context.Context, user account.User) (string, error) {
	if user.StripeCustomerID == "" {
		return "", errors.New("this account does not have a Stripe customer yet")
	}
	var response struct {
		URL string `json:"url"`
	}
	if err := s.postForm(ctx, "/billing_portal/sessions", url.Values{
		"customer": {user.StripeCustomerID}, "return_url": {s.publicURL + "/?billing=return"},
	}, &response); err != nil {
		return "", err
	}
	if response.URL == "" {
		return "", errors.New("Stripe Portal did not return a URL")
	}
	return response.URL, nil
}

func (s *Stripe) postForm(ctx context.Context, path string, form url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(s.secretKey, "")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Stripe request: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxWebhookBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxWebhookBytes {
		return errors.New("Stripe response was too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		if problem.Error.Message == "" {
			problem.Error.Message = http.StatusText(response.StatusCode)
		}
		return fmt.Errorf("Stripe returned HTTP %d: %s", response.StatusCode, problem.Error.Message)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode Stripe response: %w", err)
	}
	return nil
}

type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

type checkoutSession struct {
	Customer          string            `json:"customer"`
	Subscription      string            `json:"subscription"`
	ClientReferenceID string            `json:"client_reference_id"`
	Metadata          map[string]string `json:"metadata"`
}

type subscription struct {
	ID               string            `json:"id"`
	Customer         string            `json:"customer"`
	Status           string            `json:"status"`
	CurrentPeriodEnd int64             `json:"current_period_end"`
	Metadata         map[string]string `json:"metadata"`
	Items            struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
			Price            struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

func (s *Stripe) HandleWebhook(ctx context.Context, payload []byte, signature string) error {
	if len(payload) == 0 || len(payload) > maxWebhookBytes {
		return errors.New("invalid Stripe webhook size")
	}
	if err := verifySignature(payload, signature, s.webhookSecret, s.now()); err != nil {
		return err
	}
	var event stripeEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.ID == "" || event.Type == "" {
		return errors.New("invalid Stripe event")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.kv.Get(ctx, "billing:v1:event:"+event.ID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err := s.applyEvent(ctx, event); err != nil {
		return err
	}
	return s.kv.Put(ctx, "billing:v1:event:"+event.ID, []byte("processed"), 400*24*time.Hour)
}

func (s *Stripe) applyEvent(ctx context.Context, event stripeEvent) error {
	switch event.Type {
	case "checkout.session.completed":
		var session checkoutSession
		if err := json.Unmarshal(event.Data.Object, &session); err != nil {
			return err
		}
		userID := first(session.Metadata["user_id"], session.ClientReferenceID)
		planID := session.Metadata["plan_id"]
		if userID == "" || planID == "" {
			return errors.New("Stripe Checkout metadata omitted user_id or plan_id")
		}
		if _, ok := s.catalog.Get(planID); !ok {
			return errors.New("Stripe Checkout referenced an unknown plan")
		}
		_, err := s.accounts.UpdateBilling(ctx, userID, account.BillingUpdate{
			PlanID: planID, StripeCustomerID: session.Customer, StripeSubscriptionID: session.Subscription, SubscriptionStatus: "active",
		})
		return err
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var subscription subscription
		if err := json.Unmarshal(event.Data.Object, &subscription); err != nil {
			return err
		}
		userID := subscription.Metadata["user_id"]
		if userID == "" {
			user, err := s.accounts.FindByStripeCustomer(ctx, subscription.Customer)
			if err != nil {
				return err
			}
			userID = user.ID
		}
		planID := subscription.Metadata["plan_id"]
		periodEnd := subscription.CurrentPeriodEnd
		if len(subscription.Items.Data) > 0 {
			if plan, ok := s.catalog.PlanForPrice(subscription.Items.Data[0].Price.ID); ok {
				planID = plan.ID
			}
			if periodEnd == 0 {
				periodEnd = subscription.Items.Data[0].CurrentPeriodEnd
			}
		}
		status := subscription.Status
		if event.Type == "customer.subscription.deleted" {
			status = "canceled"
		}
		update := account.BillingUpdate{
			PlanID: planID, StripeCustomerID: subscription.Customer, StripeSubscriptionID: subscription.ID, SubscriptionStatus: status,
		}
		if periodEnd > 0 {
			update.CurrentPeriodEnd = time.Unix(periodEnd, 0).UTC()
		}
		_, err := s.accounts.UpdateBilling(ctx, userID, update)
		return err
	default:
		return nil
	}
}

func verifySignature(payload []byte, header, secret string, now time.Time) error {
	var timestamp int64
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp, _ = strconv.ParseInt(value, 10, 64)
		case "v1":
			signatures = append(signatures, value)
		}
	}
	if timestamp == 0 || len(signatures) == 0 || now.Sub(time.Unix(timestamp, 0)) > 5*time.Minute || time.Unix(timestamp, 0).Sub(now) > 5*time.Minute {
		return errors.New("invalid or expired Stripe signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		decoded, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(decoded, expected) {
			return nil
		}
	}
	return errors.New("Stripe signature verification failed")
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
