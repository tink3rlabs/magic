# Getting Started

This page walks through installing magic and making a first call.

## Requirements

- Go 1.25 or newer.
- A backing store for whichever adapter you plan to use (Postgres / DynamoDB / etc.). The in-memory adapter requires nothing.

## Install

```bash
go get github.com/tink3rlabs/magic@latest
```

## Hello, magic

```go
package main

import (
    "fmt"
    "github.com/tink3rlabs/magic/utils"
)

func main() {
    id, err := utils.NewId()
    if err != nil {
        panic(err)
    }
    fmt.Println("generated id:", id)
}
```

Run it:

```bash
go run .
```

You should see a hex-encoded, reverse-sorted UUID.

## Next steps

- Pick a [storage adapter](./storage.md).
- Learn the [Lucene filter syntax](./lucene.md).
