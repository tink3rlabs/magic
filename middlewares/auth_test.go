package middlewares

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const unauthorizedJSON = `{"status":"Unauthorized","error":"authentication required"}`

func TestEnsureValidTokenMultiProvider_AuthFailuresAreSafeAndUnauthorized(t *testing.T) {
	issuerURL, privateKey := newTestIssuer(t)

	mw := EnsureValidTokenMultiProvider(EnsureValidTokenMultiProviderConfig{
		Enabled: true,
		Providers: []ProviderConfig{
			{
				IssuerURL: issuerURL,
				Audience:  []string{"test-audience"},
			},
		},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	})
	handler := mw(next)

	tests := []struct {
		name        string
		authHeader  string
		wantStatus  int
		wantBody    string
		wantContent string
	}{
		{
			name:        "missing authorization header",
			authHeader:  "",
			wantStatus:  http.StatusUnauthorized,
			wantBody:    unauthorizedJSON,
			wantContent: "application/json",
		},
		{
			name:        "malformed authorization header",
			authHeader:  "Bearer",
			wantStatus:  http.StatusUnauthorized,
			wantBody:    unauthorizedJSON,
			wantContent: "application/json",
		},
		{
			name:        "invalid token",
			authHeader:  "Bearer not-a-jwt",
			wantStatus:  http.StatusUnauthorized,
			wantBody:    unauthorizedJSON,
			wantContent: "application/json",
		},
		{
			name:        "valid token",
			authHeader:  "Bearer " + signedTestToken(t, privateKey, issuerURL, "test-audience"),
			wantStatus:  http.StatusOK,
			wantBody:    "ok",
			wantContent: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/resource", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()

			assertDoesNotPanic(t, func() {
				handler.ServeHTTP(rec, req)
			})

			if rec.Code != tc.wantStatus {
				t.Fatalf("status mismatch: got %d want %d", rec.Code, tc.wantStatus)
			}

			if strings.TrimSpace(rec.Body.String()) != tc.wantBody {
				t.Fatalf("body mismatch: got %q want %q", rec.Body.String(), tc.wantBody)
			}

			if tc.wantContent != "" {
				if got := rec.Header().Get("Content-Type"); got != tc.wantContent {
					t.Fatalf("content-type mismatch: got %q want %q", got, tc.wantContent)
				}
			}
		})
	}
}

func assertDoesNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func signedTestToken(t *testing.T, key *rsa.PrivateKey, issuerURL string, audience string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    issuerURL,
		Subject:   "test-user-id",
		Audience:  jwt.ClaimStrings{audience},
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-kid"

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func newTestIssuer(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	n := base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.E)).Bytes())

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": "test-kid",
				"use": "sig",
				"alg": "RS256",
				"n":   n,
				"e":   e,
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jwks_uri": serverURLFromRequest(r) + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server.URL + "/", privateKey
}

func serverURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
