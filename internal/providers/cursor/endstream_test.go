package cursor

import "testing"

func TestParseConnectEndStreamTreatsEmptyAsSuccess(t *testing.T) {
	if err := ParseConnectEndStream([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := ParseConnectEndStream(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseConnectEndStream([]byte(`{"error":{"code":"unavailable"}}`)); err == nil || err.Error() != "Cursor Connect error unavailable" {
		t.Fatalf("err=%v", err)
	}
	if err := ParseConnectEndStream([]byte(`{"error":{"code":"invalid_argument"}}`)); !isInvalidArgument(err) {
		t.Fatalf("invalid_argument=%v", err)
	}
}
