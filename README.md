# Tink3rlabs Magic

[![Go Report Card](https://goreportcard.com/badge/github.com/tink3rlabs/magic)](https://goreportcard.com/report/github.com/tink3rlabs/magic)
[![Go Reference](https://pkg.go.dev/badge/github.com/tink3rlabs/magic.svg)](https://pkg.go.dev/github.com/tink3rlabs/magic)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Magic is a package containing implementation of common building blocks required when developing micro-services in go-lang.

## Goals

The goal is to move all common functionality such as speaking with storage systems, performing leader election, checking the health of a micro-service, logging in text vs. JSON, etc. out of every micro-service's code base allowing the micro-service code to focus only on business logic.

## Installation

```bash
go get github.com/tink3rlabs/magic@latest
```

## Usage

Magic exposes multiple common functionalities each in it's own package.

### Storage

This package contains everything needed to persist data in storage systems. The storage adapter provides a unified interface for different storage backends with consistent CRUD operations, migrations, and health checks.

#### Basic Usage

```go
import (
  "embed"
  "fmt"

  "github.com/google/uuid"
  "github.com/tink3rlabs/magic/storage"
)

config := map[string]string{}
s, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, config)

if err != nil {
  fmt.Println(err)
}

fmt.Println(s.Ping())
storage.NewDatabaseMigration(s).Migrate()
```

#### Storage Adapter Configuration

##### Memory Storage (Development/Testing)

```go
config := map[string]string{}
adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, config)
```

##### SQL Storage (PostgreSQL, MySQL, SQLite)

```go
// PostgreSQL
config := map[string]string{
    "provider": "postgresql",
    "host":     "localhost",
    "port":     "5432",
    "user":     "username",
    "password": "password",
    "dbname":   "database",
    "schema":   "public",
}

// MySQL
config := map[string]string{
    "provider": "mysql",
    "host":     "localhost",
    "port":     "3306",
    "user":     "username",
    "password": "password",
    "dbname":   "database",
}

// SQLite
config := map[string]string{
    "provider": "sqlite",
    "path":     "/path/to/database.db",
}

adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.SQL, config)
```

##### DynamoDB Storage

```go
config := map[string]string{
    "provider":   "dynamodb",
    "region":     "us-west-2",
    "endpoint":   "http://localhost:8000", // Optional for local testing
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
}

adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.DYNAMODB, config)
```

##### CosmosDB Storage

```go
// Individual parameters
config := map[string]string{
    "provider": "cosmosdb",
    "endpoint": "https://your-cosmosdb-account.documents.azure.com:443/",
    "key":      "your-cosmosdb-primary-key",
    "database": "magic",
}

// Or use connection string
config := map[string]string{
    "provider":          "cosmosdb",
    "connection_string": "AccountEndpoint=https://your-account.documents.azure.com:443/;AccountKey=your-key;",
    "database":          "magic",
}

// Optional: Skip TLS verification for local testing
config := map[string]string{
    "provider":       "cosmosdb",
    "endpoint":       "https://localhost:8081/",
    "key":            "your-cosmosdb-primary-key",
    "database":       "magic",
    "skip_tls_verify": "true", // Only for local testing
}

adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.COSMOSDB, config)
```

**Optional Parameters for CRUD Operations:**

The CosmosDB adapter supports dynamic partition key configuration through optional parameters:

- `pk_field`: The field name to use as the partition key in your documents (defaults to `"pk"` if not specified)
- `pk_value`: The value for the partition key
- `sort_direction`: Sort direction for List and Search operations (`"ASC"` or `"DESC"`, defaults to `"ASC"`)

**Example with Custom Partition Key:**

```go
type User struct {
    ID     string `json:"id"`
    Tenant string `json:"tenant"` // This will be used as partition key
    Name   string `json:"name"`
    Email  string `json:"email"`
}

// Create a user with tenant as partition key
user := &User{
    ID:     "user-123",
    Tenant: "acme-corp",
    Name:   "John Doe",
    Email:  "john@example.com",
}

params := map[string]any{
    "pk_field": "tenant",           // Field name in document
    "pk_value": "acme-corp",        // Partition key value
}

err := adapter.Create(user, params)

// Get user - must specify partition key
err = adapter.Get(&user, map[string]any{"id": "user-123"}, params)

// List users in a specific tenant (ascending order)
var users []User
cursor, err := adapter.List(&users, "name", map[string]any{}, 10, "", params)

// List users in descending order by name
paramsWithSort := map[string]any{
    "pk_field":      "tenant",
    "pk_value":      "acme-corp",
    "sort_direction": "DESC",
}
cursor, err = adapter.List(&users, "name", map[string]any{}, 10, "", paramsWithSort)

// Update user
user.Email = "newemail@example.com"
err = adapter.Update(user, map[string]any{"id": user.ID}, params)

// Delete user
err = adapter.Delete(&User{}, map[string]any{"id": user.ID}, params)
```

#### Storage Adapter Features

**Common Features (All Adapters):**

- CRUD operations (Create, Read, Update, Delete)
- Connection pooling and health checks
- Multi-tenant support
- Pagination with cursor-based navigation
- Search capabilities
- Custom query execution

**Memory Storage:**

- In-memory SQLite for development and testing
- No persistence across restarts
- Full migration support
- Fastest for unit tests

**SQL Storage:**

- Full migration support with version tracking
- Schema management and creation
- Support for PostgreSQL, MySQL, and SQLite
- GORM integration with advanced querying
- Transaction support
- Connection pooling

**DynamoDB Storage:**

- NoSQL document storage
- Automatic table creation based on struct types
- Attribute value marshaling/unmarshaling
- PartiQL query support
- Global and local secondary indexes
- No migration support (use application-level)

**CosmosDB Storage:**

- NoSQL document storage with SQL API using Azure SDK for Go (`azcosmos`)
- UUID generation for items without IDs
- Dynamic partition key configuration via `pk_field` and `pk_value` parameters
- Single-partition query support
- Native cursor-based pagination with continuation tokens
- SQL query support with parameterized queries
- Connection string or individual parameter configuration
- Optional TLS verification skip for local testing
- ASC/DESC sorting support
- No migration support (use application-level)

#### Storage Adapter Limitations

**Memory Storage:**

- Data lost on restart
- Single process only
- Limited by available RAM

**SQL Storage:**

- Requires database server setup
- Schema migrations required for changes
- Performance depends on database configuration

**DynamoDB Storage:**

- No migration support
- Execute method not supported
- Limited query capabilities compared to SQL
- AWS-specific service

**CosmosDB Storage:**

- Database migrations not supported
- Full-text search requires Azure Cognitive Search integration (Search method returns List results)
- Azure-specific service
- Partition key (`pk_field` and `pk_value`) must be specified for all operations

See more detailed examples in the examples folder

### Leadership

The leadership package provides distributed leader election capabilities for microservices running in clusters.

```go
import (
  "time"
  "github.com/tink3rlabs/magic/leadership"
  "github.com/tink3rlabs/magic/storage"
)

// Create leader election with storage backend
props := leadership.LeaderElectionProps{
  HeartbeatInterval: 60 * time.Second,
  StorageAdapter:    storageAdapter,
  AdditionalProps:   map[string]any{"table_name": "leadership"},
}

leaderElection := leadership.NewLeaderElection(props)

// Start leader election process
go leaderElection.Start()

// Check if this node is the leader
if leaderElection.IsLeader() {
  // Perform leader-only operations
}
```

**Features:**

- Distributed leader election using storage backends
- Configurable heartbeat intervals
- Automatic failover when leader becomes unavailable
- Support for multiple storage providers (SQL, DynamoDB)
- Thread-safe singleton pattern

### Health

The health package provides health checking capabilities for microservices and their dependencies.

```go
import (
  "github.com/tink3rlabs/magic/health"
  "github.com/tink3rlabs/magic/storage"
)

// Create health checker with storage backend
healthChecker := health.NewHealthChecker(storageAdapter)

// Check health including storage and external dependencies
err := healthChecker.Check(true, []string{
  "https://api.external-service.com/health",
  "https://cache-service.com/health",
})

if err != nil {
  // Handle health check failure
}
```

**Features:**

- Storage health verification
- External dependency health checks via HTTP
- Configurable health check parameters
- Error reporting with detailed failure information

### Logger

The logger package provides structured logging with support for both text and JSON formats.

```go
import (
  "github.com/tink3rlabs/magic/logger"
)

// Configure logger
config := &logger.Config{
  Level: logger.MapLogLevel("info"),
  JSON:  true, // Use JSON format for production
}

// Initialize global logger
logger.Init(config)

// Use standard slog functions
slog.Info("Application started", "version", "1.0.0")
slog.Error("Operation failed", "error", err)

// Fatal logging with exit
logger.Fatal("Critical error occurred")
```

**Features:**

- Structured logging with slog
- Text and JSON output formats
- Configurable log levels (debug, info, warn, error)
- Global logger configuration
- Fatal logging with automatic exit

### Middlewares

The middlewares package provides HTTP middleware components for common web service needs.

#### Authentication Middleware

```go
import (
  "github.com/tink3rlabs/magic/middlewares"
)

// Configure JWT authentication
authMiddleware := middlewares.NewAuthMiddleware(middlewares.AuthConfig{
  Audience:        []string{"https://api.example.com"},
  IssuerURL:       "https://auth.example.com/",
  ClaimsConfig:    middlewares.DefaultClaimsConfig,
  ContextKeys:     middlewares.DefaultContextKeys,
})

// Use in HTTP handler
http.Handle("/protected", authMiddleware.RequireAuth(http.HandlerFunc(protectedHandler)))
```

**Features:**

- JWT token validation with Auth0 integration
- Configurable claim mappings
- Role-based access control
- Tenant isolation support
- Context injection for user information

#### Validation Middleware

```go
import (
  "github.com/tink3rlabs/magic/middlewares"
)

// Define JSON schemas for validation
schemas := map[string]string{
  "body": `{"type": "object", "properties": {"name": {"type": "string"}}}`,
  "query": `{"type": "object", "properties": {"limit": {"type": "integer"}}}`,
}

validator := &middlewares.Validator{}
validatedHandler := validator.ValidateRequest(schemas, yourHandler)

// Use in HTTP handler
http.Handle("/api/resource", validatedHandler)
```

**Features:**

- JSON Schema validation for request body, query parameters, and URL parameters
- Configurable validation rules
- Detailed error reporting
- Support for complex validation schemas

#### Error Handler Middleware

```go
import (
  "github.com/tink3rlabs/magic/middlewares"
)

// Use error handler middleware
http.Handle("/api", middlewares.ErrorHandler(yourHandler))
```

**Features:**

- Centralized error handling
- Consistent error response format
- HTTP status code mapping
- Error logging integration

### Types

The types package provides common data structures and OpenAPI-compliant response schemas.

```go
import (
  "github.com/tink3rlabs/magic/types"
)

// Use predefined error response types
errorResponse := types.ErrorResponse{
  Status:  "Bad Request",
  Error:   "validation failed",
  Details: []string{"field is required"},
}
```

**Features:**

- Standardized error response formats
- OpenAPI-compliant schema definitions
- Common HTTP response types (BadRequest, Unauthorized, Forbidden, NotFound, ServerError)
- Consistent API response structure

### PubSub

The pubsub package provides publish-subscribe messaging capabilities.

#### SNS Publisher

```go
import (
  "github.com/tink3rlabs/magic/pubsub"
)

// Configure SNS publisher
config := map[string]string{
  "region":      "us-west-2",
  "access_key":  "your-access-key",
  "secret_key":  "your-secret-key",
  "endpoint":    "https://sns.us-west-2.amazonaws.com", // Optional for local testing
}

publisher := pubsub.GetSNSPublisher(config)

// Publish message with optional parameters
params := map[string]any{
  "groupId":    "group1",
  "dedupId":    "unique-id",
  "filterKey":  "environment",
  "filterValue": "production",
}

err := publisher.Publish("arn:aws:sns:region:account:topic", "Hello World", params)
```

**Features:**

- AWS SNS integration
- Message deduplication support
- Message grouping for FIFO topics
- Message filtering capabilities
- Configurable endpoints for local development

### MQL (Magic Query Language)

The mql package provides a simple query language parser for building dynamic queries.

```go
import (
  "github.com/tink3rlabs/magic/mql"
)

// Parse MQL expression
parser := mql.NewParser("name:john AND age:25 OR status:active")
expr, err := parser.Parse()

if err != nil {
  // Handle parsing error
}

// Use parsed expression for query building
// Supports AND, OR, NOT operators and grouping with parentheses
```

**Features:**

- Simple query language parser
- Support for logical operators (AND, OR, NOT)
- Grouping with parentheses
- Key-value pair queries
- Extensible expression tree structure

### Errors

The errors package provides typed error structures for consistent error handling.

```go
import (
  "github.com/tink3rlabs/magic/errors"
)

// Create typed errors
if userNotFound {
  return &errors.NotFound{Message: "User not found"}
}

if invalidInput {
  return &errors.BadRequest{Message: "Invalid input parameters"}
}

if unauthorized {
  return &errors.Unauthorized{Message: "Authentication required"}
}
```

**Features:**

- Typed error structures
- HTTP status code mapping
- Consistent error message format
- Easy error type checking

### Utils

The utils package provides utility functions for common operations.

```go
import (
  "github.com/tink3rlabs/magic/utils"
)

// Generate reverse-sorted UUID
id, err := utils.NewId()
if err != nil {
  // Handle error
}

// Returns a hex-encoded string with reversed byte order
// Useful for creating unique identifiers that sort in reverse order
```

**Features:**

- Reverse-sorted UUID generation
- Hex encoding utilities
- Mathematical operations for ID generation

## Contributing

Please see [CONTRIBUTING](https://github.com/tink3rlabs/magic/blob/main/CONTRIBUTING.md). Thank you, contributors!

## License

Released under the [MIT License](https://github.com/tink3rlabs/magic/blob/main/LICENSE)
