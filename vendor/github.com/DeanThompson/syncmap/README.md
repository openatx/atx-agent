syncmap
=======

[![GoDoc](https://godoc.org/github.com/DeanThompson/syncmap?status.svg)](https://godoc.org/github.com/DeanThompson/syncmap) [![Build Status](https://travis-ci.org/DeanThompson/syncmap.svg?branch=master)](https://travis-ci.org/DeanThompson/syncmap)

A thread safe map implementation for Golang

## Usage

Install with:

```bash
go get github.com/DeanThompson/syncmap
```

Example:

```go
import (
    "fmt"

    "github.com/DeanThompson/syncmap"
)

func main() {
    m := syncmap.New()
    m.Set("one", 1)
    v, ok := m.Get("one")
    fmt.Println(v, ok)  // 1, true

    v, ok = m.Get("not_exist")
    fmt.Println(v, ok)  // nil, false

    m.Set("two", 2)
    m.Set("three", "three")

    for item := range m.IterItems() {
        fmt.Println("key:", item.Key, "value:", item.Value)
    }
}
```