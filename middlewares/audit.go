package middlewares

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tink3rlabs/magic/storage"
)

var auditLoggerInstance *AuditLogger
var auditLoggerLock = &sync.Mutex{}

// AuditLog represents a single audit log entry
type AuditLog struct {
	ID           string    `json:"id" gorm:"primaryKey;column:id"`
	TenantID     string    `json:"tenant_id" gorm:"index;column:tenant_id"`
	UserID       string    `json:"user_id" gorm:"index;column:user_id"`
	UserEmail    string    `json:"user_email" gorm:"column:user_email"`
	Timestamp    time.Time `json:"timestamp" gorm:"index;column:timestamp"`
	Method       string    `json:"method" gorm:"column:method"`
	Path         string    `json:"path" gorm:"column:path"`
	ResourceType string    `json:"resource_type" gorm:"column:resource_type"`
	ResourceID   string    `json:"resource_id" gorm:"column:resource_id"`
	Action       string    `json:"action" gorm:"column:action"`
	StatusCode   int       `json:"status_code" gorm:"column:status_code"`
	IPAddress    string    `json:"ip_address" gorm:"column:ip_address"`
	UserAgent    string    `json:"user_agent" gorm:"column:user_agent"`
	RequestBody  string    `json:"request_body,omitempty" gorm:"column:request_body"`
	ResponseBody string    `json:"response_body,omitempty" gorm:"column:response_body"`
	Duration     int64     `json:"duration_us" gorm:"column:duration_us"`
	ErrorMessage string    `json:"error_message,omitempty" gorm:"column:error_message"`
	Metadata     string    `json:"metadata,omitempty" gorm:"column:metadata"`
}

// TableName specifies the table name for GORM
func (a AuditLog) TableName() string {
	// This will be overridden by the logger instance
	return "audit_logs"
}

// AuditConfig holds the configuration for the audit middleware
type AuditConfig struct {
	Enabled            bool
	TableName          string
	LogRequestBody     bool
	LogResponseBody    bool
	MaxBodySize        int
	ExcludePaths       []string
	IncludeHealthCheck bool
}

// AuditLogger manages audit trail logging
type AuditLogger struct {
	config          AuditConfig
	storageType     string
	storageProvider string
	storage         storage.StorageAdapter
}

// responseWriter wraps http.ResponseWriter to capture response details
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	body        *bytes.Buffer
	captureBody bool
}

func newResponseWriter(w http.ResponseWriter, captureBody bool) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
		captureBody:    captureBody,
	}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.captureBody {
		rw.body.Write(b)
	}
	return rw.ResponseWriter.Write(b)
}

// NewAuditLogger creates or returns the singleton AuditLogger instance
func NewAuditLogger(config AuditConfig, storageType storage.StorageAdapterType, storageConfig map[string]string) *AuditLogger {
	if !config.Enabled {
		return nil
	}

	if auditLoggerInstance == nil {
		auditLoggerLock.Lock()
		defer auditLoggerLock.Unlock()

		if auditLoggerInstance == nil {
			if config.TableName == "" {
				config.TableName = "audit_logs"
			}
			if config.MaxBodySize == 0 {
				config.MaxBodySize = 10000 // 10KB default
			}

			// Initialize storage adapter the same way services do
			storageAdapter, err := storage.StorageAdapterFactory{}.GetInstance(storageType, storageConfig)
			if err != nil {
				slog.Error("failed to create audit storage adapter", slog.Any("error", err))
				return nil
			}

			auditLoggerInstance = &AuditLogger{
				config:          config,
				storage:         storageAdapter,
				storageType:     string(storageAdapter.GetType()),
				storageProvider: string(storageAdapter.GetProvider()),
			}

			// Create audit table
			if err := auditLoggerInstance.createAuditTable(); err != nil {
				slog.Error("failed to create audit table", slog.Any("error", err))
			} else {
				slog.Info("audit logging initialized", slog.String("table", config.TableName))
			}
		}
	}
	return auditLoggerInstance
}

// createAuditTable creates the audit_logs table if it doesn't exist
func (a *AuditLogger) createAuditTable() error {
	switch a.storageType {
	case string(storage.SQL):
		var statement string
		switch a.storageProvider {
		case string(storage.POSTGRESQL):
			statement = fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %s.%s (
					id TEXT PRIMARY KEY,
					tenant_id TEXT,
					user_id TEXT,
					user_email TEXT,
					timestamp TIMESTAMP WITH TIME ZONE,
					method TEXT,
					path TEXT,
					resource_type TEXT,
					resource_id TEXT,
					action TEXT,
					status_code INTEGER,
					ip_address TEXT,
					user_agent TEXT,
					request_body TEXT,
					response_body TEXT,
					duration_us BIGINT,
					error_message TEXT,
					metadata JSONB
				);
				CREATE INDEX IF NOT EXISTS idx_audit_tenant ON %s.%s(tenant_id);
				CREATE INDEX IF NOT EXISTS idx_audit_user ON %s.%s(user_id);
				CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON %s.%s(timestamp);
				CREATE INDEX IF NOT EXISTS idx_audit_resource ON %s.%s(resource_type, resource_id);
			`,
				a.storage.GetSchemaName(), a.config.TableName,
				a.storage.GetSchemaName(), a.config.TableName,
				a.storage.GetSchemaName(), a.config.TableName,
				a.storage.GetSchemaName(), a.config.TableName,
				a.storage.GetSchemaName(), a.config.TableName,
			)
		case string(storage.MYSQL):
			statement = fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %s.%s (
					id VARCHAR(36) PRIMARY KEY,
					tenant_id VARCHAR(255),
					user_id VARCHAR(255),
					user_email VARCHAR(255),
					timestamp DATETIME,
					method VARCHAR(10),
					path VARCHAR(500),
					resource_type VARCHAR(100),
					resource_id VARCHAR(255),
					action VARCHAR(50),
					status_code INT,
					ip_address VARCHAR(45),
					user_agent VARCHAR(500),
					request_body TEXT,
					response_body TEXT,
					duration_us BIGINT,
					error_message TEXT,
					metadata JSON,
					INDEX idx_audit_tenant (tenant_id),
					INDEX idx_audit_user (user_id),
					INDEX idx_audit_timestamp (timestamp),
					INDEX idx_audit_resource (resource_type, resource_id)
				)
			`, a.storage.GetSchemaName(), a.config.TableName)
		case string(storage.SQLITE):
			statement = fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %s (
					id TEXT PRIMARY KEY,
					tenant_id TEXT,
					user_id TEXT,
					user_email TEXT,
					timestamp TEXT,
					method TEXT,
					path TEXT,
					resource_type TEXT,
					resource_id TEXT,
					action TEXT,
					status_code INTEGER,
					ip_address TEXT,
					user_agent TEXT,
					request_body TEXT,
					response_body TEXT,
					duration_us INTEGER,
					error_message TEXT,
					metadata TEXT
				);
				CREATE INDEX IF NOT EXISTS idx_audit_tenant ON %s(tenant_id);
				CREATE INDEX IF NOT EXISTS idx_audit_user ON %s(user_id);
				CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON %s(timestamp);
			`, a.config.TableName, a.config.TableName, a.config.TableName, a.config.TableName)
		}
		return a.storage.Execute(statement)

	case string(storage.DYNAMODB):
		input := &dynamodb.CreateTableInput{
			TableName: aws.String(a.config.TableName),
			AttributeDefinitions: []dynamodbTypes.AttributeDefinition{
				{AttributeName: aws.String("id"), AttributeType: dynamodbTypes.ScalarAttributeTypeS},
				{AttributeName: aws.String("tenant_id"), AttributeType: dynamodbTypes.ScalarAttributeTypeS},
				{AttributeName: aws.String("timestamp"), AttributeType: dynamodbTypes.ScalarAttributeTypeS},
			},
			KeySchema: []dynamodbTypes.KeySchemaElement{
				{AttributeName: aws.String("id"), KeyType: dynamodbTypes.KeyTypeHash},
			},
			GlobalSecondaryIndexes: []dynamodbTypes.GlobalSecondaryIndex{
				{
					IndexName: aws.String("tenant_timestamp_index"),
					KeySchema: []dynamodbTypes.KeySchemaElement{
						{AttributeName: aws.String("tenant_id"), KeyType: dynamodbTypes.KeyTypeHash},
						{AttributeName: aws.String("timestamp"), KeyType: dynamodbTypes.KeyTypeRange},
					},
					Projection: &dynamodbTypes.Projection{
						ProjectionType: dynamodbTypes.ProjectionTypeAll,
					},
					ProvisionedThroughput: &dynamodbTypes.ProvisionedThroughput{
						ReadCapacityUnits:  aws.Int64(5),
						WriteCapacityUnits: aws.Int64(5),
					},
				},
			},
			ProvisionedThroughput: &dynamodbTypes.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(5),
				WriteCapacityUnits: aws.Int64(5),
			},
		}

		adapter := a.storage.(*storage.DynamoDBAdapter)
		_, err := adapter.DB.CreateTable(context.TODO(), input)

		// Ignore error if table already exists
		var resourceInUseErr *dynamodbTypes.ResourceInUseException
		if err != nil && !errors.As(err, &resourceInUseErr) {
			return err
		}

		waiter := dynamodb.NewTableExistsWaiter(adapter.DB)
		return waiter.Wait(context.TODO(), &dynamodb.DescribeTableInput{
			TableName: aws.String(a.config.TableName),
		}, 1*time.Minute)

	default:
		return nil
	}
}

// LogEntry stores an audit log entry in the database
func (a *AuditLogger) LogEntry(entry AuditLog) error {
	if a == nil || !a.config.Enabled {
		return nil
	}

	// Async logging to not block requests
	go func() {
		var statement string
		tableName := a.config.TableName

		switch a.storageType {
		case string(storage.SQL):
			// Build fully qualified table name with schema
			if a.storage.GetSchemaName() != "" {
				tableName = fmt.Sprintf("%s.%s", a.storage.GetSchemaName(), tableName)
			}

			// Escape single quotes in string fields
			escapeQuotes := func(s string) string {
				return strings.ReplaceAll(s, "'", "''")
			}

			// Handle metadata - if empty, use NULL for PostgreSQL JSONB
			metadataValue := "NULL"
			if entry.Metadata != "" {
				metadataValue = fmt.Sprintf("'%s'", escapeQuotes(entry.Metadata))
			}

			switch a.storageProvider {
			case string(storage.POSTGRESQL):
				statement = fmt.Sprintf(`
					INSERT INTO %s (
						id, tenant_id, user_id, user_email, timestamp, method, path, 
						resource_type, resource_id, action, status_code, ip_address, 
						user_agent, request_body, response_body, duration_us, error_message, metadata
					) VALUES (
						'%s', '%s', '%s', '%s', '%s', '%s', '%s', 
						'%s', '%s', '%s', %d, '%s', 
						'%s', '%s', '%s', %d, '%s', %s
					)`,
					tableName,
					entry.ID,
					escapeQuotes(entry.TenantID),
					escapeQuotes(entry.UserID),
					escapeQuotes(entry.UserEmail),
					entry.Timestamp.Format("2006-01-02 15:04:05.999999-07:00"),
					entry.Method,
					escapeQuotes(entry.Path),
					escapeQuotes(entry.ResourceType),
					escapeQuotes(entry.ResourceID),
					entry.Action,
					entry.StatusCode,
					entry.IPAddress,
					escapeQuotes(entry.UserAgent),
					escapeQuotes(entry.RequestBody),
					escapeQuotes(entry.ResponseBody),
					entry.Duration,
					escapeQuotes(entry.ErrorMessage),
					metadataValue,
				)
			case string(storage.MYSQL):
				statement = fmt.Sprintf(`
					INSERT INTO %s (
						id, tenant_id, user_id, user_email, timestamp, method, path, 
						resource_type, resource_id, action, status_code, ip_address, 
						user_agent, request_body, response_body, duration_us, error_message, metadata
					) VALUES (
						'%s', '%s', '%s', '%s', '%s', '%s', '%s', 
						'%s', '%s', '%s', %d, '%s', 
						'%s', '%s', '%s', %d, '%s', %s
					)`,
					tableName,
					entry.ID,
					escapeQuotes(entry.TenantID),
					escapeQuotes(entry.UserID),
					escapeQuotes(entry.UserEmail),
					entry.Timestamp.Format("2006-01-02 15:04:05"),
					entry.Method,
					escapeQuotes(entry.Path),
					escapeQuotes(entry.ResourceType),
					escapeQuotes(entry.ResourceID),
					entry.Action,
					entry.StatusCode,
					entry.IPAddress,
					escapeQuotes(entry.UserAgent),
					escapeQuotes(entry.RequestBody),
					escapeQuotes(entry.ResponseBody),
					entry.Duration,
					escapeQuotes(entry.ErrorMessage),
					metadataValue,
				)
			case string(storage.SQLITE):
				statement = fmt.Sprintf(`
					INSERT INTO %s (
						id, tenant_id, user_id, user_email, timestamp, method, path, 
						resource_type, resource_id, action, status_code, ip_address, 
						user_agent, request_body, response_body, duration_us, error_message, metadata
					) VALUES (
						'%s', '%s', '%s', '%s', '%s', '%s', '%s', 
						'%s', '%s', '%s', %d, '%s', 
						'%s', '%s', '%s', %d, '%s', %s
					)`,
					tableName,
					entry.ID,
					escapeQuotes(entry.TenantID),
					escapeQuotes(entry.UserID),
					escapeQuotes(entry.UserEmail),
					entry.Timestamp.Format("2006-01-02 15:04:05"),
					entry.Method,
					escapeQuotes(entry.Path),
					escapeQuotes(entry.ResourceType),
					escapeQuotes(entry.ResourceID),
					entry.Action,
					entry.StatusCode,
					entry.IPAddress,
					escapeQuotes(entry.UserAgent),
					escapeQuotes(entry.RequestBody),
					escapeQuotes(entry.ResponseBody),
					entry.Duration,
					escapeQuotes(entry.ErrorMessage),
					metadataValue,
				)
			}

			if err := a.storage.Execute(statement); err != nil {
				slog.Error("failed to create audit log entry",
					slog.Any("error", err),
					slog.String("entry_id", entry.ID),
				)
			}

		case string(storage.DYNAMODB):
			// For DynamoDB, use the storage.Create method
			if err := a.storage.Create(&entry); err != nil {
				slog.Error("failed to create audit log entry",
					slog.Any("error", err),
					slog.String("entry_id", entry.ID),
				)
			}
		}
	}()

	return nil
}

// shouldExcludePath checks if the path should be excluded from audit logging
func (a *AuditLogger) shouldExcludePath(path string) bool {
	if !a.config.IncludeHealthCheck {
		if strings.HasPrefix(path, "/health") {
			return true
		}
	}

	for _, excluded := range a.config.ExcludePaths {
		if strings.HasPrefix(path, excluded) {
			return true
		}
	}
	return false
}

// extractResourceInfo extracts resource type and ID from the request
func extractResourceInfo(r *http.Request) (string, string, string) {
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Default action based on method
	action := strings.ToUpper(r.Method)

	var resourceType, resourceID string

	// Try to extract resource type and ID from path
	// Example: /documents/123 -> resource_type: "documents", resource_id: "123"
	if len(parts) >= 1 {
		resourceType = parts[0]
	}
	if len(parts) >= 2 {
		// Check if second part looks like an ID (not a sub-resource)
		if !strings.Contains(parts[1], "-") || len(parts[1]) > 5 {
			resourceID = parts[1]
		}
	}

	// Try to get ID from chi URL params
	if resourceID == "" {
		routeCtx := chi.RouteContext(r.Context())
		if routeCtx != nil {
			for i, key := range routeCtx.URLParams.Keys {
				if key == "id" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "Id") {
					resourceID = routeCtx.URLParams.Values[i]
					break
				}
			}
		}
	}

	// Refine action based on method and context
	switch r.Method {
	case "POST":
		if resourceID != "" {
			action = "UPDATE" // POST to /resource/id is often an update
		} else {
			action = "CREATE"
		}
	case "PUT":
		action = "UPDATE"
	case "PATCH":
		action = "PATCH"
	case "DELETE":
		action = "DELETE"
	case "GET":
		if resourceID != "" {
			action = "READ"
		} else {
			action = "LIST"
		}
	}

	return resourceType, resourceID, action
}

// getClientIP extracts the real client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// AuditMiddleware returns a middleware that logs all requests to the audit table
func AuditMiddleware(config AuditConfig, storageType storage.StorageAdapterType, storageConfig map[string]string) func(http.Handler) http.Handler {
	logger := NewAuditLogger(config, storageType, storageConfig)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if audit logging is disabled
			if logger == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Skip excluded paths
			if logger.shouldExcludePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			startTime := time.Now()

			// Extract user context
			ctx := r.Context()
			tenantID := GetTenantFromContext(ctx)
			userID := GetUserIDFromContext(ctx)
			userEmail := GetEmailFromContext(ctx)

			// Extract resource info
			resourceType, resourceID, action := extractResourceInfo(r)

			// Capture request body if configured
			var requestBody string
			if logger.config.LogRequestBody && r.Body != nil {
				bodyBytes, err := io.ReadAll(r.Body)
				if err == nil {
					r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
					if len(bodyBytes) > 0 {
						requestBody = truncateString(string(bodyBytes), logger.config.MaxBodySize)
					}
				}
			}

			// Wrap response writer to capture status and body
			rw := newResponseWriter(w, logger.config.LogResponseBody)

			// Process request
			next.ServeHTTP(rw, r)

			// Calculate duration in microseconds for better precision on fast requests
			duration := time.Since(startTime).Microseconds()

			// Build audit entry
			entry := AuditLog{
				ID:           uuid.New().String(),
				TenantID:     tenantID,
				UserID:       userID,
				UserEmail:    userEmail,
				Timestamp:    startTime,
				Method:       r.Method,
				Path:         r.URL.Path,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Action:       action,
				StatusCode:   rw.statusCode,
				IPAddress:    getClientIP(r),
				UserAgent:    truncateString(r.UserAgent(), 500),
				RequestBody:  requestBody,
				Duration:     duration,
			}

			// Capture response body if configured
			if logger.config.LogResponseBody && rw.body.Len() > 0 {
				entry.ResponseBody = truncateString(rw.body.String(), logger.config.MaxBodySize)
			}

			// Log the entry
			err := logger.LogEntry(entry)
			if err != nil {
				slog.Error("failed to log audit entry", slog.Any("error", err))
			}
		})
	}
}
