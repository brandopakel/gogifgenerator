// Package account owns GoGIF users, commercial plans, and usage accounting.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/store"
)

const (
	PlanGuest   = "guest"
	PlanFree    = "free"
	PlanCreator = "creator"
	PlanPro     = "pro"
	PlanLegacy  = "legacy"
)

var (
	ErrSignInRequired  = errors.New("sign in is required")
	ErrUpgradeRequired = errors.New("a paid plan is required")
	ErrQuotaExceeded   = errors.New("generation credits are exhausted")
	ErrQualityLimit    = errors.New("requested quality exceeds the plan limit")
)

type User struct {
	ID                   string    `json:"id"`
	Issuer               string    `json:"issuer"`
	Subject              string    `json:"subject"`
	Email                string    `json:"email"`
	EmailVerified        bool      `json:"email_verified"`
	Name                 string    `json:"name,omitempty"`
	PictureURL           string    `json:"picture_url,omitempty"`
	Role                 string    `json:"role,omitempty"`
	PlanID               string    `json:"plan_id"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
	SubscriptionStatus   string    `json:"subscription_status,omitempty"`
	CurrentPeriodEnd     time.Time `json:"current_period_end,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type Identity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	PictureURL    string
}

type BillingUpdate struct {
	PlanID               string
	StripeCustomerID     string
	StripeSubscriptionID string
	SubscriptionStatus   string
	CurrentPeriodEnd     time.Time
}

type Principal struct {
	ID            string `json:"-"`
	UserID        string `json:"user_id,omitempty"`
	Email         string `json:"email,omitempty"`
	Name          string `json:"name,omitempty"`
	Role          string `json:"role,omitempty"`
	PlanID        string `json:"plan_id"`
	Authenticated bool   `json:"authenticated"`
	Legacy        bool   `json:"-"`
}

func (p Principal) OwnerID() string {
	if !p.Authenticated {
		return ""
	}
	return p.UserID
}

func (p Principal) IsAdmin() bool { return p.Role == "admin" }

type Plan struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Currency          string `json:"currency"`
	MonthlyPriceCents int    `json:"monthly_price_cents"`
	Credits           int    `json:"credits"`
	CreditPeriod      string `json:"credit_period"`
	MaxDimension      int    `json:"max_dimension"`
	MaxFrames         int    `json:"max_frames"`
	LibraryAssets     int    `json:"library_assets"`
	LibraryBytes      int64  `json:"library_bytes"`
	Semantic          bool   `json:"semantic"`
	Models3D          bool   `json:"models_3d"`
	Studio            bool   `json:"studio"`
	Paid              bool   `json:"paid"`
	PurchaseEnabled   bool   `json:"purchase_enabled"`
	StripePriceID     string `json:"-"`
}

type Catalog struct {
	plans map[string]Plan
}

type CatalogOptions struct {
	CreatorPriceID string
	ProPriceID     string
	CreatorCents   int
	ProCents       int
}

func NewCatalog(options CatalogOptions) Catalog {
	creatorCents := options.CreatorCents
	if creatorCents <= 0 {
		creatorCents = 1500
	}
	proCents := options.ProCents
	if proCents <= 0 {
		proCents = 3900
	}
	plans := []Plan{
		{ID: PlanGuest, Name: "Guest", Description: "Search and edit before signing up for cloud creation.", Currency: "usd", Credits: 3, CreditPeriod: "day", MaxDimension: 320, MaxFrames: 8},
		{ID: PlanFree, Name: "Free", Description: "A private starter library and a few realistic creations.", Currency: "usd", Credits: 10, CreditPeriod: "month", MaxDimension: 480, MaxFrames: 12, LibraryAssets: 25, LibraryBytes: 100 << 20, Semantic: true},
		{ID: PlanCreator, Name: "Creator", Description: "High-quality GIFs, 3D creation, and a larger private library.", Currency: "usd", MonthlyPriceCents: creatorCents, Credits: 150, CreditPeriod: "month", MaxDimension: 720, MaxFrames: 18, LibraryAssets: 500, LibraryBytes: 5 << 30, Semantic: true, Models3D: true, Paid: true, PurchaseEnabled: options.CreatorPriceID != "", StripePriceID: options.CreatorPriceID},
		{ID: PlanPro, Name: "Pro", Description: "Maximum quality, more 3D capacity, and experimental scene tools where available.", Currency: "usd", MonthlyPriceCents: proCents, Credits: 500, CreditPeriod: "month", MaxDimension: 720, MaxFrames: 24, LibraryAssets: 2500, LibraryBytes: 25 << 30, Semantic: true, Models3D: true, Studio: true, Paid: true, PurchaseEnabled: options.ProPriceID != "", StripePriceID: options.ProPriceID},
		{ID: PlanLegacy, Name: "Local owner", Description: "Unmetered self-hosted compatibility mode.", Currency: "usd", Credits: 1 << 30, CreditPeriod: "month", MaxDimension: 720, MaxFrames: 60, LibraryAssets: 1 << 30, LibraryBytes: 1 << 60, Semantic: true, Models3D: true, Studio: true},
	}
	result := Catalog{plans: make(map[string]Plan, len(plans))}
	for _, plan := range plans {
		result.plans[plan.ID] = plan
	}
	return result
}

func (c Catalog) Get(id string) (Plan, bool) {
	plan, ok := c.plans[id]
	return plan, ok
}

func (c Catalog) Public() []Plan {
	ids := []string{PlanFree, PlanCreator, PlanPro}
	result := make([]Plan, 0, len(ids))
	for _, id := range ids {
		if plan, ok := c.Get(id); ok {
			result = append(result, plan)
		}
	}
	return result
}

func (c Catalog) PlanForPrice(priceID string) (Plan, bool) {
	for _, plan := range c.plans {
		if priceID != "" && plan.StripePriceID == priceID {
			return plan, true
		}
	}
	return Plan{}, false
}

type Operation struct {
	Kind   string
	Mode   string
	Width  int
	Height int
	Frames int
}

type Quote struct {
	Cost int  `json:"cost"`
	Plan Plan `json:"plan"`
}

func (c Catalog) Quote(principal Principal, operation Operation) (Quote, error) {
	planID := principal.PlanID
	if principal.Legacy {
		planID = PlanLegacy
	}
	plan, ok := c.Get(planID)
	if !ok {
		plan = c.plans[PlanFree]
	}
	if operation.Width > plan.MaxDimension || operation.Height > plan.MaxDimension || operation.Frames > plan.MaxFrames {
		return Quote{Plan: plan}, fmt.Errorf("%w: %s allows up to %dpx and %d frames", ErrQualityLimit, plan.Name, plan.MaxDimension, plan.MaxFrames)
	}
	mode := strings.ToLower(strings.TrimSpace(operation.Mode))
	if mode == "semantic" && !plan.Semantic {
		if !principal.Authenticated {
			return Quote{Plan: plan}, fmt.Errorf("%w: create a free account to generate subject-aware GIFs", ErrSignInRequired)
		}
		return Quote{Plan: plan}, fmt.Errorf("%w: subject-aware GIF creation is not included in %s", ErrUpgradeRequired, plan.Name)
	}
	if operation.Kind == "model" && !plan.Models3D {
		if !principal.Authenticated {
			return Quote{Plan: plan}, fmt.Errorf("%w: sign in and choose a paid plan to create 3D models", ErrSignInRequired)
		}
		return Quote{Plan: plan}, fmt.Errorf("%w: 3D creation requires Creator or Pro", ErrUpgradeRequired)
	}
	if mode == "studio" && !plan.Studio {
		return Quote{Plan: plan}, fmt.Errorf("%w: the experimental scene renderer requires Pro", ErrUpgradeRequired)
	}
	cost := 1
	switch {
	case operation.Kind == "model":
		cost = 50
	case mode == "studio":
		cost = 30
	case mode == "semantic":
		cost = 5
		if operation.Width > 480 || operation.Height > 480 || operation.Frames > 12 {
			cost = 8
		}
	}
	return Quote{Cost: cost, Plan: plan}, nil
}

type Repository struct {
	kv  store.KV
	mu  sync.Mutex
	now func() time.Time
}

func NewRepository(kv store.KV) *Repository {
	return &Repository{kv: kv, now: time.Now}
}

func (r *Repository) UpsertIdentity(ctx context.Context, identity Identity) (User, error) {
	identity.Issuer = strings.TrimSpace(identity.Issuer)
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	if identity.Issuer == "" || identity.Subject == "" || identity.Email == "" || !identity.EmailVerified {
		return User{}, errors.New("verified issuer, subject, and email are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	indexKey := subjectKey(identity.Issuer, identity.Subject)
	if data, err := r.kv.Get(ctx, indexKey); err == nil {
		return r.updateIdentity(ctx, string(data), identity)
	} else if !errors.Is(err, store.ErrNotFound) {
		return User{}, err
	}
	id, err := newUserID()
	if err != nil {
		return User{}, err
	}
	now := r.now().UTC()
	user := User{
		ID: id, Issuer: identity.Issuer, Subject: identity.Subject, Email: identity.Email,
		EmailVerified: true, Name: clean(identity.Name, 120), PictureURL: clean(identity.PictureURL, 2048),
		PlanID: PlanFree, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.putUser(ctx, user); err != nil {
		return User{}, err
	}
	if err := r.kv.Put(ctx, indexKey, []byte(user.ID), 0); err != nil {
		return User{}, fmt.Errorf("index account identity: %w", err)
	}
	return user, nil
}

func (r *Repository) updateIdentity(ctx context.Context, id string, identity Identity) (User, error) {
	user, err := r.getUnlocked(ctx, id)
	if err != nil {
		return User{}, err
	}
	user.Email = identity.Email
	user.EmailVerified = true
	user.Name = clean(identity.Name, 120)
	user.PictureURL = clean(identity.PictureURL, 2048)
	user.UpdatedAt = r.now().UTC()
	return user, r.putUser(ctx, user)
}

func (r *Repository) Get(ctx context.Context, id string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getUnlocked(ctx, id)
}

func (r *Repository) FindByStripeCustomer(ctx context.Context, customerID string) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := r.kv.Get(ctx, stripeCustomerKey(customerID))
	if err != nil {
		return User{}, err
	}
	return r.getUnlocked(ctx, string(data))
}

func (r *Repository) UpdateBilling(ctx context.Context, userID string, update BillingUpdate) (User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, err := r.getUnlocked(ctx, userID)
	if err != nil {
		return User{}, err
	}
	if update.PlanID != "" {
		user.PlanID = update.PlanID
	}
	if update.StripeCustomerID != "" {
		user.StripeCustomerID = update.StripeCustomerID
	}
	if update.StripeSubscriptionID != "" {
		user.StripeSubscriptionID = update.StripeSubscriptionID
	}
	user.SubscriptionStatus = update.SubscriptionStatus
	user.CurrentPeriodEnd = update.CurrentPeriodEnd
	user.UpdatedAt = r.now().UTC()
	if !subscriptionGrantsPaidAccess(user.SubscriptionStatus) {
		user.PlanID = PlanFree
	}
	if err := r.putUser(ctx, user); err != nil {
		return User{}, err
	}
	if user.StripeCustomerID != "" {
		if err := r.kv.Put(ctx, stripeCustomerKey(user.StripeCustomerID), []byte(user.ID), 0); err != nil {
			return User{}, fmt.Errorf("index Stripe customer: %w", err)
		}
	}
	return user, nil
}

func subscriptionGrantsPaidAccess(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "trialing", "past_due":
		return true
	default:
		return false
	}
}

func (r *Repository) getUnlocked(ctx context.Context, id string) (User, error) {
	data, err := r.kv.Get(ctx, userKey(id))
	if err != nil {
		return User{}, err
	}
	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return User{}, fmt.Errorf("decode account: %w", err)
	}
	return user, nil
}

func (r *Repository) putUser(ctx context.Context, user User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return r.kv.Put(ctx, userKey(user.ID), data, 0)
}

type Usage struct {
	ActorID      string                 `json:"actor_id"`
	Period       string                 `json:"period"`
	Used         int                    `json:"used"`
	Reservations map[string]Reservation `json:"reservations,omitempty"`
}

type Reservation struct {
	ID        string    `json:"id"`
	Cost      int       `json:"cost"`
	CreatedAt time.Time `json:"created_at"`
}

type Ledger struct {
	kv  store.KV
	mu  sync.Mutex
	now func() time.Time
}

func NewLedger(kv store.KV) *Ledger { return &Ledger{kv: kv, now: time.Now} }

func (l *Ledger) Reserve(ctx context.Context, actorID string, plan Plan, cost int) (Reservation, Usage, error) {
	if actorID == "" || cost < 1 {
		return Reservation{}, Usage{}, errors.New("actor and positive cost are required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	period := usagePeriod(plan.CreditPeriod, now)
	usage, err := l.read(ctx, actorID, period)
	if err != nil {
		return Reservation{}, Usage{}, err
	}
	for id, held := range usage.Reservations {
		if now.Sub(held.CreatedAt) > time.Hour {
			delete(usage.Reservations, id)
		}
	}
	held := 0
	for _, reservation := range usage.Reservations {
		held += reservation.Cost
	}
	if usage.Used+held+cost > plan.Credits {
		return Reservation{}, usage, fmt.Errorf("%w: %d of %d credits remain", ErrQuotaExceeded, max(0, plan.Credits-usage.Used-held), plan.Credits)
	}
	id, err := randomID("use_", 12)
	if err != nil {
		return Reservation{}, Usage{}, err
	}
	reservation := Reservation{ID: id, Cost: cost, CreatedAt: now}
	usage.Reservations[id] = reservation
	if err := l.write(ctx, usage); err != nil {
		return Reservation{}, Usage{}, err
	}
	return reservation, usage, nil
}

func (l *Ledger) Complete(ctx context.Context, actorID string, plan Plan, reservationID string) error {
	return l.finish(ctx, actorID, plan, reservationID, true)
}

func (l *Ledger) Release(ctx context.Context, actorID string, plan Plan, reservationID string) error {
	return l.finish(ctx, actorID, plan, reservationID, false)
}

func (l *Ledger) Summary(ctx context.Context, actorID string, plan Plan) (Usage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.read(ctx, actorID, usagePeriod(plan.CreditPeriod, l.now().UTC()))
}

func (l *Ledger) finish(ctx context.Context, actorID string, plan Plan, reservationID string, consume bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	usage, err := l.read(ctx, actorID, usagePeriod(plan.CreditPeriod, l.now().UTC()))
	if err != nil {
		return err
	}
	reservation, ok := usage.Reservations[reservationID]
	if !ok {
		return nil
	}
	delete(usage.Reservations, reservationID)
	if consume {
		usage.Used += reservation.Cost
	}
	return l.write(ctx, usage)
}

func (l *Ledger) read(ctx context.Context, actorID, period string) (Usage, error) {
	usage := Usage{ActorID: actorID, Period: period, Reservations: make(map[string]Reservation)}
	data, err := l.kv.Get(ctx, usageKey(actorID, period))
	if errors.Is(err, store.ErrNotFound) {
		return usage, nil
	}
	if err != nil {
		return Usage{}, err
	}
	if err := json.Unmarshal(data, &usage); err != nil {
		return Usage{}, fmt.Errorf("decode usage: %w", err)
	}
	if usage.Reservations == nil {
		usage.Reservations = make(map[string]Reservation)
	}
	return usage, nil
}

func (l *Ledger) write(ctx context.Context, usage Usage) error {
	data, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	return l.kv.Put(ctx, usageKey(usage.ActorID, usage.Period), data, 400*24*time.Hour)
}

func usagePeriod(period string, now time.Time) string {
	if period == "day" {
		return now.Format("2006-01-02")
	}
	return now.Format("2006-01")
}

func userKey(id string) string { return "account:v1:user:" + cleanKey(id) }

func subjectKey(issuer, subject string) string {
	digest := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return "account:v1:subject:" + hex.EncodeToString(digest[:])
}

func stripeCustomerKey(id string) string { return "account:v1:stripe:" + cleanKey(id) }

func usageKey(actorID, period string) string {
	return "usage:v1:" + cleanKey(actorID) + ":" + cleanKey(period)
}

func cleanKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "invalid"
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
}

func clean(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func newUserID() (string, error) { return randomID("usr_", 16) }

func randomID(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFrom(ctx context.Context) Principal {
	principal, _ := ctx.Value(principalContextKey{}).(Principal)
	return principal
}
