package mailcontent

import (
	"encoding/hex"
	"errors"
	"strings"
)

// ErrSourceIntegrity identifies a missing/invalid checksum or bytes whose digest
// disagrees with their content-addressed object key. It contains no raw content.
var ErrSourceIntegrity = errors.New("raw source integrity check failed")

// ValidateSourceHash checks the checksum representation and the sha256 namespace
// emitted by rawobject.Key. Opaque legacy and per-receipt ingress keys do not
// encode a digest: a valid hash alone cannot prove their stored bytes unchanged.
func ValidateSourceHash(key, hash string) error {
	if len(hash) != 64 {
		return ErrSourceIntegrity
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ErrSourceIntegrity
	}
	if strings.HasPrefix(key, "sha256/") {
		digest := strings.ToLower(hash)
		if key != "sha256/"+digest[:2]+"/"+digest+".eml" {
			return ErrSourceIntegrity
		}
	}
	return nil
}
