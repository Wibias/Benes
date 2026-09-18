package cursor

import (
	"errors"
	"testing"
)

func TestResolveDestinationRequiresHTTPSBeforeCredentials(t *testing.T) {
	got, err := ResolveDestination("", HTTPVersion2)
	if err != nil || got != DefaultAPI {
		t.Fatalf("default=%q %v", got, err)
	}
	if _, err := ResolveDestination("http://api2.cursor.sh", HTTPVersion1Dot1); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("cleartext http1=%v", err)
	}
	if _, err := ResolveDestination("https://user:tok@api2.cursor.sh", HTTPVersion2); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("userinfo=%v", err)
	}
	https, err := ResolveDestination("https://api2.cursor.sh/", HTTPVersion1Dot1)
	if err != nil || https != DefaultAPI {
		t.Fatalf("https pin=%q %v", https, err)
	}
}
