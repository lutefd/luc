package auth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "luc"

var ErrNotFound = errors.New("no credential found")

// Set stores a credential for the given provider ID in the OS keychain.
func Set(providerID, key string) error {
	return keyring.Set(service, providerID, key)
}

// Get retrieves a credential for the given provider ID from the OS keychain.
// Returns ErrNotFound if no credential is stored.
func Get(providerID string) (string, error) {
	key, err := keyring.Get(service, providerID)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return key, err
}

// Delete removes the credential for the given provider ID from the OS keychain.
func Delete(providerID string) error {
	err := keyring.Delete(service, providerID)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// List returns all provider IDs that have credentials stored.
// go-keyring has no enumerate API so we check against known provider IDs.
func List(knownProviderIDs []string) []string {
	var found []string
	for _, id := range knownProviderIDs {
		if _, err := Get(id); err == nil {
			found = append(found, id)
		}
	}
	return found
}
