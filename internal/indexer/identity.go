package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func ChunkID(pageID string, chunkIndex int) string {
	return fmt.Sprintf("%s:%06d", pageID, chunkIndex)
}

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
