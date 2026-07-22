package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"
)

func TestResolveCredentialReferencesRecursively(t *testing.T) {
	id := strings.Repeat("a", 64)
	input := []byte(`{"providers":{"main":{"apiKey":"keyring://ruby/` + id + `"}},"agents":[{"headers":{"Authorization":"keyring://ruby/` + id + `"}}],"channels":[{"config":{"appSecret":"keyring://ruby/` + id + `"}}]}`)
	resolved, err := resolveCredentialReferences(input, func(got string) (string, error) {
		if got != id {
			t.Fatalf("credential id = %q", got)
		}
		return "secret-value", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal(resolved, &root); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resolved), credentialReferencePrefix) || strings.Count(string(resolved), "secret-value") != 3 {
		t.Fatalf("references were not resolved: %s", resolved)
	}
}

func TestSystemCredentialReferenceIntegration(t *testing.T) {
	if os.Getenv("MIYA_KEYRING_INTEGRATION") != "1" {
		t.Skip("set MIYA_KEYRING_INTEGRATION=1 to exercise the Ruby Desktop credential service")
	}
	id := strings.Repeat("e", 56) + time.Now().UTC().Format("15040500")
	if err := keyring.Set(credentialServiceName, id, "integration-secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keyring.Delete(credentialServiceName, id) })
	resolved, err := ResolveCredentialReferences([]byte(`{"apiKey":"keyring://ruby/` + id + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resolved), "integration-secret") {
		t.Fatalf("credential not resolved: %s", resolved)
	}
}

func TestResolveCredentialReferencesRejectsInvalidAndMissing(t *testing.T) {
	if _, err := resolveCredentialReferences([]byte(`{"apiKey":"keyring://ruby/nope"}`), func(string) (string, error) { return "", nil }); err == nil {
		t.Fatal("invalid reference accepted")
	}
	id := strings.Repeat("b", 64)
	if _, err := resolveCredentialReferences([]byte(`{"apiKey":"keyring://ruby/`+id+`"}`), func(string) (string, error) { return "", errors.New("missing") }); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing credential error = %v", err)
	}
}
