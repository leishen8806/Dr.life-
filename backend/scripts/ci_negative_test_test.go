package scripts

import "testing"

func TestCIIntentionalFailure(t *testing.T) {
	t.Fatal("intentional CI negative test")
}
