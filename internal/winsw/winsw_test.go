package winsw

import (
	"strings"
	"testing"
)

func TestBuildXMLStartsBenes(t *testing.T) {
	xml := BuildXML(`C:\benes.exe`, `C:\Users\ws\.benes`, 18080, map[string]string{
		"USERNAME": "ws", "USERDOMAIN": "BOX", "PATH": `C:\Windows`,
	})
	if !strings.Contains(xml, `C:\benes.exe`) || !strings.Contains(xml, "start --port 18080") {
		t.Fatalf("xml=%s", xml)
	}
	if !strings.Contains(xml, "BENES_HOME") || !strings.Contains(xml, "ws") {
		t.Fatalf("account/home missing: %s", xml)
	}
}

func TestParseStatusFailClosedOnGarbage(t *testing.T) {
	if ParseStatus("NonExistent") != StatusNonexistent {
		t.Fatal("nonexistent")
	}
	if ParseStatus("Started") != StatusStarted {
		t.Fatal("started")
	}
	if ParseStatus("Stopped") != StatusStopped {
		t.Fatal("stopped")
	}
	if ParseStatus("access denied") != StatusUnknown {
		t.Fatal("garbage must be unknown")
	}
}

func TestVerifySHA256RejectsMismatch(t *testing.T) {
	if err := VerifySHA256([]byte("not-winsw")); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestLocalSystemStartNameIsRejected(t *testing.T) {
	if !LocalSystemStartName("SERVICE_START_NAME : LocalSystem") {
		t.Fatal("LocalSystem not detected")
	}
	if LocalSystemStartName("SERVICE_START_NAME : .\\ws") {
		t.Fatal("user account treated as LocalSystem")
	}
	if !StartNameMatchesUser("SERVICE_START_NAME : .\\ws", "ws") {
		t.Fatal("user mismatch")
	}
}
