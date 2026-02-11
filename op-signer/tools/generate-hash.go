package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run generate-hash.go <password>")
		fmt.Println("Example: go run generate-hash.go mySecurePassword123")
		os.Exit(1)
	}

	password := os.Args[1]

	// Generate bcrypt hash with cost 10 (default)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("Error generating hash: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== Bcrypt Password Hash Generated ===")
	fmt.Printf("\nPassword: %s\n", password)
	fmt.Printf("Hash:     %s\n", string(hash))
	fmt.Printf("Hash:     %d\n", len(hash))
	fmt.Println("\nAdd this hash to your StatefulSet YAML:")
	fmt.Printf("  - name: API_PASSWORD_HASH\n")
	fmt.Printf("    value: \"%s\"\n", string(hash))
	fmt.Println("\n⚠️  Keep the password secret! Only share the hash in your manifests.")
}
