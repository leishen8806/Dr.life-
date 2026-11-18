package notifications

import "testing"

func TestStartAppLinkEscapesOpaquePayload(t *testing.T) {
	got, err := StartAppLink("@drlife_buyer_bot", "order/01?end=buyer")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://t.me/drlife_buyer_bot?startapp=order%2F01%3Fend%3Dbuyer"
	if got != want {
		t.Fatalf("link=%q, want %q", got, want)
	}
}

func TestStartAppLinkRejectsInvalidPublicInputs(t *testing.T) {
	for _, test := range []struct {
		name, username, payload string
	}{
		{name: "missing username", payload: "order-1"},
		{name: "bad username", username: "bot-name", payload: "order-1"},
		{name: "missing payload", username: "drlife_bot", payload: " "},
		{name: "oversized payload", username: "drlife_bot", payload: "12345678901234567890123456789012345678901234567890123456789012345"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := StartAppLink(test.username, test.payload); err == nil {
				t.Fatal("invalid deep-link input was accepted")
			}
		})
	}
}
