package utils

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

// NewId generates a new UUID and returns it as a string.
// It reverses the byte order of the UUID and encodes it in hexadecimal format.
// This is useful for creating unique identifiers that are sorted in reverse order.
func NewId() (*string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}
	max := new(big.Int).Lsh(big.NewInt(1), 128) // 2^128
	val := new(big.Int).SetBytes(id[:])
	reversed := new(big.Int).Sub(max, val)
	encoded := hex.EncodeToString(reversed.Bytes())
	return &encoded, nil
}
