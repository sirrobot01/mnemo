package safety

import "testing"

func TestRejectIfSecretBlocksTokenLikeContent(t *testing.T) {
	err := RejectIfSecret("api_key = abcdefghijklmnopqrstuvwxyz123456")
	if err == nil {
		t.Fatal("expected secret content to be rejected")
	}
}

func TestRejectIfSecretAllowsNormalMemory(t *testing.T) {
	if err := RejectIfSecret("Run tests with go test ./..."); err != nil {
		t.Fatalf("expected normal content to pass, got %v", err)
	}
}
