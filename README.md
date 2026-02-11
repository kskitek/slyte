# slyte

An opinionated Go "library" for SQLite database setup with built-in migrations.

## Why?

Because I was copying this piece over and over..

## Installation

```bash
go get github.com/kskitek/slyte
```

## Quick Start

```go
package main

import (
    "github.com/kskitek/slyte"
)

func main() {
    // I prefer eager initialization with panics
    db.Init()
    
    // Separating read and write allows concurrrent reads with WAL
    writeDB := db.GetWriteDB()
    readDB := db.GetReadDB()
    
    // don't do this - you'll get sqli problems.. I use sqlc
    writeDB.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
    
    rows, _ := readDB.Query("SELECT * FROM users")
    defer rows.Close()
}
```

## Migrations

- only "up" migrations
- all migrations are put in `migrations` directory
- migrations are embedded in the binary
- migration files must follow naming format `[0-9]{4}_description.sql`

## Configuration

- configuration is ENV-driven
- if anything fails, it will panic
- all options have defaults that work well for my use cases
  - PRAGMAs are applied automatically, set for reasonable production usage

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_PATH` | `sqlite.db` | Database file path |
| `DB_JOURNAL_MODE` | `WAL` | SQLite journal mode |
| `DB_SYNCHRONOUS` | `NORMAL` | Synchronous setting (OFF, NORMAL, FULL) |
| `DB_CACHE_SIZE_MB` | `250` | Cache size in megabytes |
| `DB_FOREIGN_KEYS` | `true` | Enable foreign key constraints |
| `DB_BUSY_TIMEOUT` | `5000` | Busy timeout in milliseconds |
| `DB_TEMP_STORE` | `memory` | Temporary storage location (file, memory) |
| `DB_MAX_WRITE_CONNS` | `1` | Maximum write connections (recommended: 1) |
| `DB_MAX_READ_CONNS` | `4` | Minimum read connections (actual: max(4, NumCPU)) |

## Connection Pooling Strategy

- **Write Database**: Limited to 1 connection
- **Read Database**: Minimum of 4 connections, or `runtime.NumCPU()` if higher

## Contributing

Contributions are welcome but not encouraged (-:
I don't plan to maintain it really.
