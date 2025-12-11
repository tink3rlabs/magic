package middlewares

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSetDefaultContextKeys(t *testing.T) {
	originalKeys := DefaultContextKeys
	defer func() {
		DefaultContextKeys = originalKeys
	}()

	type args struct {
		keys ContextKeys
	}
	tests := []struct {
		name     string
		args     args
		validate func(t *testing.T)
	}{
		{
			name: "set all keys",
			args: args{
				keys: ContextKeys{
					Tenant:    "custom-tenant-key",
					UserId:    "custom-user-key",
					UserEmail: "custom-email-key",
					Roles:     "custom-roles-key",
					Groups:    "custom-groups-key",
				},
			},
			validate: func(t *testing.T) {
				if DefaultContextKeys.Tenant != "custom-tenant-key" {
					t.Errorf("DefaultContextKeys.Tenant = %v, want custom-tenant-key", DefaultContextKeys.Tenant)
				}
			},
		},
		{
			name: "set partial keys",
			args: args{
				keys: ContextKeys{
					Tenant: "partial-tenant",
				},
			},
			validate: func(t *testing.T) {
				if DefaultContextKeys.Tenant != "partial-tenant" {
					t.Errorf("DefaultContextKeys.Tenant = %v, want partial-tenant", DefaultContextKeys.Tenant)
				}
			},
		},
		{
			name: "set nil keys should not override",
			args: args{
				keys: ContextKeys{},
			},
			validate: func(t *testing.T) {
				if DefaultContextKeys.Tenant == nil {
					t.Error("DefaultContextKeys.Tenant should not be nil")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaultContextKeys(tt.args.keys)
			if tt.validate != nil {
				tt.validate(t)
			}
		})
	}
}

func TestSetDefaultClaimsConfig(t *testing.T) {
	originalConfig := DefaultClaimsConfig
	defer func() {
		DefaultClaimsConfig = originalConfig
	}()

	type args struct {
		cfg ClaimsConfig
	}
	tests := []struct {
		name     string
		args     args
		validate func(t *testing.T)
	}{
		{
			name: "set all claim keys",
			args: args{
				cfg: ClaimsConfig{
					TenantIdKey: "custom_org_id",
					EmailKey:    "custom_email",
					RolesKey:    "custom_roles",
					GroupsKey:   "custom_groups",
				},
			},
			validate: func(t *testing.T) {
				if DefaultClaimsConfig.TenantIdKey != "custom_org_id" {
					t.Errorf("DefaultClaimsConfig.TenantIdKey = %v, want custom_org_id", DefaultClaimsConfig.TenantIdKey)
				}
			},
		},
		{
			name: "set partial claim keys",
			args: args{
				cfg: ClaimsConfig{
					RolesKey: "custom_roles",
				},
			},
			validate: func(t *testing.T) {
				if DefaultClaimsConfig.RolesKey != "custom_roles" {
					t.Errorf("DefaultClaimsConfig.RolesKey = %v, want custom_roles", DefaultClaimsConfig.RolesKey)
				}
			},
		},
		{
			name: "empty strings should not override",
			args: args{
				cfg: ClaimsConfig{},
			},
			validate: func(t *testing.T) {
				if DefaultClaimsConfig.TenantIdKey == "" {
					t.Error("DefaultClaimsConfig.TenantIdKey should not be empty")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaultClaimsConfig(tt.args.cfg)
			if tt.validate != nil {
				tt.validate(t)
			}
		})
	}
}

func Test_customClaims_UnmarshalJSON(t *testing.T) {
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		c       *customClaims
		args    args
		wantErr bool
		want    map[string]any
	}{
		{
			name: "valid JSON with scope",
			c:    &customClaims{},
			args: args{
				data: []byte(`{"scope":"read write","org_id":"tenant123","email":"test@example.com"}`),
			},
			wantErr: false,
			want: map[string]any{
				"org_id": "tenant123",
				"email":  "test@example.com",
			},
		},
		{
			name: "valid JSON without scope",
			c:    &customClaims{},
			args: args{
				data: []byte(`{"org_id":"tenant123"}`),
			},
			wantErr: false,
			want: map[string]any{
				"org_id": "tenant123",
			},
		},
		{
			name: "invalid JSON",
			c:    &customClaims{},
			args: args{
				data: []byte(`{invalid json}`),
			},
			wantErr: true,
		},
		{
			name: "empty JSON object",
			c:    &customClaims{},
			args: args{
				data: []byte(`{}`),
			},
			wantErr: false,
			want:    map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.UnmarshalJSON(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("customClaims.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.want != nil {
				for k, v := range tt.want {
					if got, ok := tt.c.Claims[k]; !ok || got != v {
						t.Errorf("customClaims.Claims[%q] = %v, want %v", k, got, v)
					}
				}
				if tt.c.Scope != "" && len(tt.args.data) > 0 {
					var raw map[string]any
					json.Unmarshal(tt.args.data, &raw)
					if scope, ok := raw["scope"]; ok {
						if tt.c.Scope != scope {
							t.Errorf("customClaims.Scope = %v, want %v", tt.c.Scope, scope)
						}
					}
				}
			}
		})
	}
}

func Test_customClaims_Validate(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		c       *customClaims
		args    args
		wantErr bool
	}{
		{
			name:    "always returns nil",
			c:       &customClaims{},
			args:    args{ctx: context.Background()},
			wantErr: false,
		},
		{
			name:    "nil context",
			c:       &customClaims{},
			args:    args{ctx: nil},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Validate(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("customClaims.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureValidToken(t *testing.T) {
	tests := []struct {
		name string
		cfg  EnsureValidTokenConfig
		want func(http.Handler) http.Handler
		// For functions, we test behavior instead of comparing
		testBehavior func(t *testing.T, middleware func(http.Handler) http.Handler)
	}{
		{
			name: "disabled middleware should pass through",
			cfg: EnsureValidTokenConfig{
				Enabled: false,
			},
			testBehavior: func(t *testing.T, middleware func(http.Handler) http.Handler) {
				called := false
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
				})
				wrapped := middleware(handler)
				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				wrapped.ServeHTTP(w, req)
				if !called {
					t.Error("handler should be called when middleware is disabled")
				}
			},
		},
		{
			name: "enabled middleware should return function",
			cfg: EnsureValidTokenConfig{
				Enabled:   true,
				IssuerURL: "https://example.com",
				Audience:  []string{"test"},
			},
			testBehavior: func(t *testing.T, middleware func(http.Handler) http.Handler) {
				if middleware == nil {
					t.Error("middleware should not be nil")
				}
				// Just verify it's a function - actual token validation would require real JWT setup
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
				wrapped := middleware(handler)
				if wrapped == nil {
					t.Error("wrapped handler should not be nil")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureValidToken(tt.cfg)
			if got == nil {
				t.Fatal("EnsureValidToken() returned nil")
			}
			if tt.testBehavior != nil {
				tt.testBehavior(t, got)
			}
		})
	}
}

func TestTenantRequestContext(t *testing.T) {
	tests := []struct {
		name string
		next http.Handler
		testBehavior func(t *testing.T, handler http.Handler)
	}{
		{
			name: "injects tenant from validated claims",
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenant := GetTenantFromContext(r.Context())
				if tenant != "test-tenant" {
					t.Errorf("GetTenantFromContext() = %v, want test-tenant", tenant)
				}
			}),
			testBehavior: func(t *testing.T, handler http.Handler) {
				claims := &validatedClaims{
					Subject: "user123",
					CustomClaims: &customClaims{
						Claims: map[string]any{
							"org_id": "test-tenant",
						},
					},
				}
				ctx := context.WithValue(context.Background(), contextKeyValidatedClaims{}, claims)
				req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			},
		},
		{
			name: "injects empty tenant when no claims",
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tenant := GetTenantFromContext(r.Context())
				if tenant != "" {
					t.Errorf("GetTenantFromContext() = %v, want empty string", tenant)
				}
			}),
			testBehavior: func(t *testing.T, handler http.Handler) {
				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TenantRequestContext(tt.next)
			if got == nil {
				t.Fatal("TenantRequestContext() returned nil")
			}
			tt.testBehavior(t, got)
		})
	}
}

func TestUserRequestContext(t *testing.T) {
	tests := []struct {
		name string
		next http.Handler
		testBehavior func(t *testing.T, handler http.Handler)
	}{
		{
			name: "injects user info from validated claims",
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				userID := GetUserIDFromContext(r.Context())
				email := GetEmailFromContext(r.Context())
				roles := GetRolesFromContext(r.Context())
				if userID != "user123" {
					t.Errorf("GetUserIDFromContext() = %v, want user123", userID)
				}
				if email != "test@example.com" {
					t.Errorf("GetEmailFromContext() = %v, want test@example.com", email)
				}
				if !reflect.DeepEqual(roles, []string{"admin"}) {
					t.Errorf("GetRolesFromContext() = %v, want [admin]", roles)
				}
			}),
			testBehavior: func(t *testing.T, handler http.Handler) {
				claims := &validatedClaims{
					Subject: "user123",
					CustomClaims: &customClaims{
						Claims: map[string]any{
							"email": "test@example.com",
							"roles": []string{"admin"},
						},
					},
				}
				ctx := context.WithValue(context.Background(), contextKeyValidatedClaims{}, claims)
				req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			},
		},
		{
			name: "injects empty values when no claims",
			next: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if GetUserIDFromContext(r.Context()) != "" {
					t.Error("GetUserIDFromContext() should return empty string")
				}
			}),
			testBehavior: func(t *testing.T, handler http.Handler) {
				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UserRequestContext(tt.next)
			if got == nil {
				t.Fatal("UserRequestContext() returned nil")
			}
			tt.testBehavior(t, got)
		})
	}
}

func TestRequireRole(t *testing.T) {
	tests := []struct {
		name     string
		roleName string
		testBehavior func(t *testing.T, middleware func(http.Handler) http.Handler)
	}{
		{
			name:     "allows access when user has role",
			roleName: "admin",
			testBehavior: func(t *testing.T, middleware func(http.Handler) http.Handler) {
				called := false
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
				})
				wrapped := middleware(handler)
				claims := &validatedClaims{
					Subject: "user123",
					CustomClaims: &customClaims{
						Claims: map[string]any{
							"roles": []string{"admin", "user"},
						},
					},
				}
				ctx := context.WithValue(context.Background(), contextKeyValidatedClaims{}, claims)
				req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				w := httptest.NewRecorder()
				wrapped.ServeHTTP(w, req)
				if !called {
					t.Error("handler should be called when user has required role")
				}
				if w.Code != http.StatusOK {
					t.Errorf("status code = %v, want %v", w.Code, http.StatusOK)
				}
			},
		},
		{
			name:     "denies access when user lacks role",
			roleName: "admin",
			testBehavior: func(t *testing.T, middleware func(http.Handler) http.Handler) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("handler should not be called")
				})
				wrapped := middleware(handler)
				claims := &validatedClaims{
					Subject: "user123",
					CustomClaims: &customClaims{
						Claims: map[string]any{
							"roles": []string{"user"},
						},
					},
				}
				ctx := context.WithValue(context.Background(), contextKeyValidatedClaims{}, claims)
				req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
				w := httptest.NewRecorder()
				wrapped.ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Errorf("status code = %v, want %v", w.Code, http.StatusForbidden)
				}
			},
		},
		{
			name:     "returns unauthorized when no claims",
			roleName: "admin",
			testBehavior: func(t *testing.T, middleware func(http.Handler) http.Handler) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("handler should not be called")
				})
				wrapped := middleware(handler)
				req := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				wrapped.ServeHTTP(w, req)
				if w.Code != http.StatusUnauthorized {
					t.Errorf("status code = %v, want %v", w.Code, http.StatusUnauthorized)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequireRole(tt.roleName)
			if got == nil {
				t.Fatal("RequireRole() returned nil")
			}
			tt.testBehavior(t, got)
		})
	}
}

func TestGetUserIDFromContext(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "returns user ID from context",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.UserId, "user123"),
			},
			want: "user123",
		},
		{
			name: "returns empty string when not set",
			args: args{
				ctx: context.Background(),
			},
			want: "",
		},
		{
			name: "returns empty string when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.UserId, 123),
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetUserIDFromContext(tt.args.ctx); got != tt.want {
				t.Errorf("GetUserIDFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEmailFromContext(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "returns email from context",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.UserEmail, "test@example.com"),
			},
			want: "test@example.com",
		},
		{
			name: "returns empty string when not set",
			args: args{
				ctx: context.Background(),
			},
			want: "",
		},
		{
			name: "returns empty string when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.UserEmail, 123),
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetEmailFromContext(tt.args.ctx); got != tt.want {
				t.Errorf("GetEmailFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetRolesFromContext(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "returns roles from context",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Roles, []string{"admin", "user"}),
			},
			want: []string{"admin", "user"},
		},
		{
			name: "returns nil when not set",
			args: args{
				ctx: context.Background(),
			},
			want: nil,
		},
		{
			name: "returns nil when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Roles, "not-a-slice"),
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetRolesFromContext(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetRolesFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetGroupsFromContext(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "returns groups from context",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Groups, []string{"group1", "group2"}),
			},
			want: []string{"group1", "group2"},
		},
		{
			name: "returns nil when not set",
			args: args{
				ctx: context.Background(),
			},
			want: nil,
		},
		{
			name: "returns nil when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Groups, "not-a-slice"),
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetGroupsFromContext(tt.args.ctx); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetGroupsFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTenantFromContext(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "returns tenant from context",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Tenant, "tenant123"),
			},
			want: "tenant123",
		},
		{
			name: "returns empty string when not set",
			args: args{
				ctx: context.Background(),
			},
			want: "",
		},
		{
			name: "returns empty string when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), DefaultContextKeys.Tenant, 123),
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetTenantFromContext(tt.args.ctx); got != tt.want {
				t.Errorf("GetTenantFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getValidatedClaims(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want *validatedClaims
	}{
		{
			name: "returns validated claims from context",
			args: args{
				ctx: context.WithValue(context.Background(), contextKeyValidatedClaims{}, &validatedClaims{
					Subject: "user123",
					CustomClaims: &customClaims{},
				}),
			},
			want: &validatedClaims{
				Subject:      "user123",
				CustomClaims: &customClaims{},
			},
		},
		{
			name: "returns nil when not set",
			args: args{
				ctx: context.Background(),
			},
			want: nil,
		},
		{
			name: "returns nil when wrong type",
			args: args{
				ctx: context.WithValue(context.Background(), contextKeyValidatedClaims{}, "not-claims"),
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getValidatedClaims(tt.args.ctx)
			if tt.want == nil {
				if got != nil {
					t.Errorf("getValidatedClaims() = %v, want nil", got)
				}
			} else {
				if got == nil || got.Subject != tt.want.Subject {
					t.Errorf("getValidatedClaims() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func Test_getRoles(t *testing.T) {
	type args struct {
		c   *customClaims
		cfg ClaimsConfig
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "returns roles from []string",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"roles": []string{"admin", "user"},
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: []string{"admin", "user"},
		},
		{
			name: "returns roles from []any",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"roles": []any{"admin", "user"},
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: []string{"admin", "user"},
		},
		{
			name: "returns nil when not found",
			args: args{
				c: &customClaims{
					Claims: map[string]any{},
				},
				cfg: DefaultClaimsConfig,
			},
			want: nil,
		},
		{
			name: "returns nil for wrong type",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"roles": "not-a-slice",
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: nil,
		},
		{
			name: "uses custom roles key",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"custom_roles": []string{"admin"},
					},
				},
				cfg: ClaimsConfig{RolesKey: "custom_roles"},
			},
			want: []string{"admin"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getRoles(tt.args.c, tt.args.cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRoles() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getGroups(t *testing.T) {
	type args struct {
		c   *customClaims
		cfg ClaimsConfig
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "returns groups from []string",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"groups": []string{"group1", "group2"},
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: []string{"group1", "group2"},
		},
		{
			name: "returns groups from []any",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"groups": []any{"group1", "group2"},
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: []string{"group1", "group2"},
		},
		{
			name: "returns nil when not found",
			args: args{
				c: &customClaims{
					Claims: map[string]any{},
				},
				cfg: DefaultClaimsConfig,
			},
			want: nil,
		},
		{
			name: "uses custom groups key",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"custom_groups": []string{"group1"},
					},
				},
				cfg: ClaimsConfig{GroupsKey: "custom_groups"},
			},
			want: []string{"group1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getGroups(tt.args.c, tt.args.cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getGroups() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getTenant(t *testing.T) {
	type args struct {
		c   *customClaims
		cfg ClaimsConfig
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "returns tenant from claims",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"org_id": "tenant123",
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "tenant123",
		},
		{
			name: "returns empty string when not found",
			args: args{
				c: &customClaims{
					Claims: map[string]any{},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "",
		},
		{
			name: "returns empty string for wrong type",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"org_id": 123,
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "",
		},
		{
			name: "uses custom tenant key",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"custom_org": "tenant123",
					},
				},
				cfg: ClaimsConfig{TenantIdKey: "custom_org"},
			},
			want: "tenant123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getTenant(tt.args.c, tt.args.cfg); got != tt.want {
				t.Errorf("getTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getEmail(t *testing.T) {
	type args struct {
		c   *customClaims
		cfg ClaimsConfig
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "returns email from claims",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"email": "test@example.com",
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "test@example.com",
		},
		{
			name: "returns empty string when not found",
			args: args{
				c: &customClaims{
					Claims: map[string]any{},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "",
		},
		{
			name: "returns empty string for wrong type",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"email": 123,
					},
				},
				cfg: DefaultClaimsConfig,
			},
			want: "",
		},
		{
			name: "uses custom email key",
			args: args{
				c: &customClaims{
					Claims: map[string]any{
						"custom_email": "test@example.com",
					},
				},
				cfg: ClaimsConfig{EmailKey: "custom_email"},
			},
			want: "test@example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getEmail(tt.args.c, tt.args.cfg); got != tt.want {
				t.Errorf("getEmail() = %v, want %v", got, tt.want)
			}
		})
	}
}
