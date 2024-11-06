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

This package contains everything needed to persist data in storage systems. For example to write data to an in-memory database you would instantiate the storage system as follows:

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

See more detailed examples in the examples folder

### Leadership

TODO

### Health

TODO

### Logger

TODO

### Middlewares

TODO

### Types

TODO

## Contributing

Please see [CONTRIBUTING](https://github.com/tink3rlabs/magic/blob/main/CONTRIBUTING.md). Thank you, contributors!

## License

Released under the [MIT License](https://github.com/tink3rlabs/magic/blob/main/LICENSE)
