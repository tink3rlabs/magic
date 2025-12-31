package middlewares

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	jwtmiddleware "github.com/auth0/go-jwt-middleware/v2"
	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/tink3rlabs/magic/logger"
)

// internal context key types

type contextKeyTenant struct{}
type contextKeyUserId struct{}
type contextKeyUserEmail struct{}
type contextKeyRoles struct{}
type contextKeyGroups struct{}
type contextKeyValidatedClaims struct{}

// ClaimsConfig allows you to configure claim keys. These must match the keys used by your IDP
type ClaimsConfig struct {
	TenantIdKey string
	EmailKey    string
	RolesKey    string
	GroupsKey   string
}

// DefaultClaimsConfig provides default claim keys used by the middleware
// You can override these using SetDefaultClaimsConfig
var DefaultClaimsConfig = ClaimsConfig{
	TenantIdKey: "org_id",
	EmailKey:    "email",
	RolesKey:    "roles",
	GroupsKey:   "groups",
}

// ContextKey is used to store the validated claims in the request context safely
type ContextKeys struct {
	Tenant    any
	UserId    any
	UserEmail any
	Roles     any
	Groups    any
}

// DefaultContextKeys provides default context keys used by the middleware
// You can override these using SetDefaultContextKeys
// Note: The types are intentionally set to `any` to allow flexibility in the values stored
// You can use the exported functions to retrieve values from the context
// Example: GetUserIDFromContext(ctx) will return the user ID as a string
var DefaultContextKeys = ContextKeys{
	Tenant:    contextKeyTenant{},
	UserId:    contextKeyUserId{},
	UserEmail: contextKeyUserEmail{},
	Roles:     contextKeyRoles{},
	Groups:    contextKeyGroups{},
}

// Setters for overrides

// SetDefaultContextKeys allows you to override the default context keys used by the middleware
func SetDefaultContextKeys(keys ContextKeys) {
	if keys.Tenant != nil {
		DefaultContextKeys.Tenant = keys.Tenant
	}
	if keys.UserId != nil {
		DefaultContextKeys.UserId = keys.UserId
	}
	if keys.UserEmail != nil {
		DefaultContextKeys.UserEmail = keys.UserEmail
	}
	if keys.Roles != nil {
		DefaultContextKeys.Roles = keys.Roles
	}
	if keys.Groups != nil {
		DefaultContextKeys.Groups = keys.Groups
	}
}

// SetDefaultClaimsConfig allows you to override the default claims configuration used by the middleware
func SetDefaultClaimsConfig(cfg ClaimsConfig) {
	if cfg.RolesKey != "" {
		DefaultClaimsConfig.RolesKey = cfg.RolesKey
	}
	if cfg.GroupsKey != "" {
		DefaultClaimsConfig.GroupsKey = cfg.GroupsKey
	}
	if cfg.TenantIdKey != "" {
		DefaultClaimsConfig.TenantIdKey = cfg.TenantIdKey
	}
	if cfg.EmailKey != "" {
		DefaultClaimsConfig.EmailKey = cfg.EmailKey
	}
}

// customClaims holds dynamic JWT claims
type customClaims struct {
	Scope  string         `json:"scope"`
	Claims map[string]any `json:"-"`
}

func (c *customClaims) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.Claims = make(map[string]any)
	for k, v := range raw {
		if k == "scope" {
			if s, ok := v.(string); ok {
				c.Scope = s
			}
			continue
		}
		c.Claims[k] = v
	}
	return nil
}

// For now we don't need to implement validation for custom claims
func (c *customClaims) Validate(ctx context.Context) error {
	return nil
}

type validatedClaims struct {
	Subject      string
	CustomClaims *customClaims
	ClaimsConfig ClaimsConfig
}

// Core Middleware

// EnsureValidTokenConfig holds the configuration for the EnsureValidToken middleware
type EnsureValidTokenConfig struct {
	Enabled          bool
	IssuerURL        string
	Audience         []string
	AllowedClockSkew time.Duration
}

// EnsureValidToken is a middleware that validates JWT tokens and injects claims into the request context
func EnsureValidToken(cfg EnsureValidTokenConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}
	}

	issuerURL, err := url.Parse(cfg.IssuerURL)
	if err != nil {
		logger.Fatal("failed to parse issuer URL", slog.Any("error", err.Error()))
	}

	provider := jwks.NewCachingProvider(issuerURL, 5*time.Minute)

	jwtValidator, err := validator.New(
		provider.KeyFunc,
		validator.RS256,
		issuerURL.String(),
		cfg.Audience,
		validator.WithCustomClaims(func() validator.CustomClaims { return &customClaims{} }),
		validator.WithAllowedClockSkew(cfg.AllowedClockSkew),
	)
	if err != nil {
		logger.Fatal("failed to set up JWT validator", slog.Any("error", err.Error()))
	}

	errorHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("JWT validation failed", slog.Any("error", err.Error()))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"Unauthorized","error":"authentication required"}`))
	}

	middleware := jwtmiddleware.New(jwtValidator.ValidateToken, jwtmiddleware.WithErrorHandler(errorHandler))

	return func(next http.Handler) http.Handler {
		return middleware.CheckJWT(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw := r.Context().Value(jwtmiddleware.ContextKey{}); raw != nil {
				if vclaims, ok := raw.(*validator.ValidatedClaims); ok {
					sub := vclaims.RegisteredClaims.Subject
					cc, _ := vclaims.CustomClaims.(*customClaims)
					ctx := context.WithValue(r.Context(), contextKeyValidatedClaims{}, &validatedClaims{
						Subject:      sub,
						CustomClaims: cc,
					})
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// ProviderConfig holds the configuration for a single JWT provider
type ProviderConfig struct {
	IssuerURL        string
	Audience         []string
	AllowedClockSkew time.Duration
	// ClaimsConfig specifies the claim keys for this provider. If not set, DefaultClaimsConfig is used.
	ClaimsConfig *ClaimsConfig
}

// EnsureValidTokenMultiProviderConfig holds the configuration for the multi-provider middleware
type EnsureValidTokenMultiProviderConfig struct {
	Enabled   bool
	Providers []ProviderConfig
}

// EnsureValidTokenMultiProvider is a middleware that validates JWT tokens against multiple providers
// It tries each provider in sequence until one succeeds, or returns unauthorized if all fail.
// This is useful when you need to accept tokens from multiple OIDC providers (e.g., Auth0 and another provider).
//
// Example usage:
//
//	cfg := EnsureValidTokenMultiProviderConfig{
//	  Enabled: true,
//	  Providers: []ProviderConfig{
//	    {
//	      IssuerURL: "https://your-tenant.auth0.com/",
//	      Audience: []string{"https://api.example.com"},
//	      AllowedClockSkew: 5 * time.Second,
//	      // Uses DefaultClaimsConfig (org_id, email, roles, groups)
//	    },
//	    {
//	      IssuerURL: "https://other-provider.com",
//	      Audience: []string{"https://api.example.com"},
//	      AllowedClockSkew: 5 * time.Second,
//	      ClaimsConfig: &ClaimsConfig{
//	        TenantIdKey: "tenant_id",  // Different claim key for this provider
//	        EmailKey:    "email",
//	        RolesKey:    "user_roles",
//	        GroupsKey:   "user_groups",
//	      },
//	    },
//	  },
//	}
//	router.Use(middlewares.EnsureValidTokenMultiProvider(cfg))
func EnsureValidTokenMultiProvider(cfg EnsureValidTokenMultiProviderConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}
	}

	if len(cfg.Providers) == 0 {
		logger.Fatal("EnsureValidTokenMultiProvider requires at least one provider configuration")
	}

	// Create validators for each provider
	type validatorWithConfig struct {
		validator    *validator.Validator
		claimsConfig ClaimsConfig
	}
	validators := make([]validatorWithConfig, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		issuerURL, err := url.Parse(providerCfg.IssuerURL)
		if err != nil {
			logger.Fatal("failed to parse issuer URL", slog.String("issuer", providerCfg.IssuerURL), slog.Any("error", err.Error()))
		}

		provider := jwks.NewCachingProvider(issuerURL, 5*time.Minute)

		jwtValidator, err := validator.New(
			provider.KeyFunc,
			validator.RS256,
			issuerURL.String(),
			providerCfg.Audience,
			validator.WithCustomClaims(func() validator.CustomClaims { return &customClaims{} }),
			validator.WithAllowedClockSkew(providerCfg.AllowedClockSkew),
		)
		if err != nil {
			logger.Fatal("failed to set up JWT validator", slog.String("issuer", providerCfg.IssuerURL), slog.Any("error", err.Error()))
		}

		// Use provider-specific claims config or fall back to default
		claimsConfig := DefaultClaimsConfig
		if providerCfg.ClaimsConfig != nil {
			claimsConfig = *providerCfg.ClaimsConfig
		}

		validators = append(validators, validatorWithConfig{
			validator:    jwtValidator,
			claimsConfig: claimsConfig,
		})
	}

	errorHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("JWT validation failed against all providers", slog.Any("error", err.Error()))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":"Unauthorized","error":"authentication required"}`))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				errorHandler(w, r, nil)
				return
			}

			// Support "Bearer <token>" format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				errorHandler(w, r, nil)
				return
			}

			token := parts[1]

			// Try validating against each provider
			var lastErr error
			for i, vc := range validators {
				ctx := r.Context()
				claims, err := vc.validator.ValidateToken(ctx, token)
				if err != nil {
					slog.Debug("Token validation failed for provider",
						slog.Int("provider_index", i),
						slog.String("issuer", cfg.Providers[i].IssuerURL),
						slog.Any("error", err.Error()))
					lastErr = err
					continue
				}

				// Token is valid for this provider
				if vclaims, ok := claims.(*validator.ValidatedClaims); ok {
					sub := vclaims.RegisteredClaims.Subject
					cc, _ := vclaims.CustomClaims.(*customClaims)
					validated := &validatedClaims{
						Subject:      sub,
						CustomClaims: cc,
						ClaimsConfig: vc.claimsConfig,
					}
					ctx := context.WithValue(r.Context(), contextKeyValidatedClaims{}, validated)
					r = r.WithContext(ctx)
					next.ServeHTTP(w, r)
					return
				}
			}

			// All providers failed
			errorHandler(w, r, lastErr)
		})
	}
}

// Middlewares to inject claims into context

// TenantRequestContext injects the tenant ID from claims into the request context
func TenantRequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		claims := getValidatedClaims(r.Context())
		tenantId := ""
		if claims != nil && claims.CustomClaims != nil {
			claimsConfig := getClaimsConfig(claims.ClaimsConfig)
			tenantId = getTenant(claims.CustomClaims, claimsConfig)
		}
		ctx := context.WithValue(r.Context(), DefaultContextKeys.Tenant, tenantId)
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// UserRequestContext injects user information from claims into the request context
// It sets user ID, email, roles, and groups in the context
func UserRequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		claims := getValidatedClaims(r.Context())
		sub := ""
		email := ""
		roles := []string{}
		groups := []string{}
		if claims != nil && claims.CustomClaims != nil {
			claimsConfig := getClaimsConfig(claims.ClaimsConfig)
			sub = claims.Subject
			email = getEmail(claims.CustomClaims, claimsConfig)
			roles = getRoles(claims.CustomClaims, claimsConfig)
			groups = getGroups(claims.CustomClaims, claimsConfig)
		}
		ctx := context.WithValue(r.Context(), DefaultContextKeys.UserId, sub)
		ctx = context.WithValue(ctx, DefaultContextKeys.UserEmail, email)
		ctx = context.WithValue(ctx, DefaultContextKeys.Roles, roles)
		ctx = context.WithValue(ctx, DefaultContextKeys.Groups, groups)
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// RequireRole is a middleware that checks if the user has the required role
// If the user does not have the role, it returns a 403 Forbidden response
// If the user is not authenticated, it returns a 401 Unauthorized response
// Usage: router.Use(RequireRole("admin"))
// If the user has the role, it calls the next handler in the chain
func RequireRole(roleName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			claims := getValidatedClaims(r.Context())
			if claims == nil || claims.CustomClaims == nil {
				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(http.StatusUnauthorized)
				_, _ = rw.Write([]byte(`{"status":"Unauthorized","error":"authentication required"}`))
				return
			}
			claimsConfig := getClaimsConfig(claims.ClaimsConfig)
			roles := getRoles(claims.CustomClaims, claimsConfig)
			for _, role := range roles {
				if role == roleName {
					next.ServeHTTP(rw, r)
					return
				}
			}
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusForbidden)
			_, _ = rw.Write([]byte(`{"status":"Forbidden","error":"you are not allowed to perform this action"}`))
		})
	}
}

// Exported context accessors

// GetUserIDFromContext retrieves the user ID from the request context
// It returns an empty string if the user ID is not set in the context
func GetUserIDFromContext(ctx context.Context) string {
	if val := ctx.Value(DefaultContextKeys.UserId); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// GetUserEmailFromContext retrieves the user email from the request context
// It returns an empty string if the email is not set in the context
func GetEmailFromContext(ctx context.Context) string {
	if val := ctx.Value(DefaultContextKeys.UserEmail); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// GetRolesFromContext retrieves the user roles from the request context
// It returns an empty slice if the roles are not set in the context
func GetRolesFromContext(ctx context.Context) []string {
	if val := ctx.Value(DefaultContextKeys.Roles); val != nil {
		if s, ok := val.([]string); ok {
			return s
		}
	}
	return nil
}

// GetGroupsFromContext retrieves the user groups from the request context
// It returns an empty slice if the groups are not set in the context
func GetGroupsFromContext(ctx context.Context) []string {
	if val := ctx.Value(DefaultContextKeys.Groups); val != nil {
		if s, ok := val.([]string); ok {
			return s
		}
	}
	return nil
}

// GetTenantFromContext retrieves the tenant ID from the request context
// It returns an empty string if the tenant ID is not set in the context
func GetTenantFromContext(ctx context.Context) string {
	if val := ctx.Value(DefaultContextKeys.Tenant); val != nil {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// Unexported helpers

func getValidatedClaims(ctx context.Context) *validatedClaims {
	val := ctx.Value(contextKeyValidatedClaims{})
	if claims, ok := val.(*validatedClaims); ok {
		return claims
	}
	return nil
}

func getRoles(c *customClaims, cfg ClaimsConfig) []string {
	if val, ok := c.Claims[cfg.RolesKey]; ok {
		switch v := val.(type) {
		case []any:
			str := make([]string, len(v))
			for i, r := range v {
				str[i], _ = r.(string)
			}
			return str
		case []string:
			return v
		}
	}
	return nil
}

func getGroups(c *customClaims, cfg ClaimsConfig) []string {
	if val, ok := c.Claims[cfg.GroupsKey]; ok {
		switch v := val.(type) {
		case []any:
			str := make([]string, len(v))
			for i, g := range v {
				str[i], _ = g.(string)
			}
			return str
		case []string:
			return v
		}
	}
	return nil
}

func getTenant(c *customClaims, cfg ClaimsConfig) string {
	if val, ok := c.Claims[cfg.TenantIdKey]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func getEmail(c *customClaims, cfg ClaimsConfig) string {
	if val, ok := c.Claims[cfg.EmailKey]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// getClaimsConfig returns the provided claims config if it's been explicitly set (non-zero),
// otherwise returns DefaultClaimsConfig
func getClaimsConfig(cfg ClaimsConfig) ClaimsConfig {
	// Check if any field is set (non-empty), indicating it was explicitly configured
	if cfg.TenantIdKey != "" || cfg.EmailKey != "" || cfg.RolesKey != "" || cfg.GroupsKey != "" {
		return cfg
	}
	return DefaultClaimsConfig
}
