package google

import "github.com/Wibias/Benes/internal/credentialpool"

// Test-only helpers for google_test (external tests) that must drive combo.Walker
// against a real KeyPool without importing combo into package google.

func SetTestOrigin(c *Client, origin string) {
	if c != nil {
		c.testOrigin = origin
	}
}

func TestingSelectKey(c *Client) (ref, key string) {
	if c == nil || c.keys == nil {
		return "", ""
	}
	return c.keys.Select()
}

func TestingReportKeyRateLimited(c *Client, ref string) {
	if c == nil || c.keys == nil || c.keys.pool == nil {
		return
	}
	c.keys.pool.ReportFailure(ref, credentialpool.Failure{Class: credentialpool.FailureRateLimited})
}
