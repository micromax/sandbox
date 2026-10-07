package main

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"time"
)

func main() {
	msg := "micromax/sandbox high-performance isolation"
	hash := sha256.Sum256([]byte(msg))

	fmt.Println("=== Go Language Pack ===")
	fmt.Printf("Go Version : %s\n", runtime.Version())
	fmt.Printf("OS / Arch  : %s / %s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("SHA256     : %x\n", hash)
	fmt.Printf("Timestamp  : %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Println("Status: OK")
}
