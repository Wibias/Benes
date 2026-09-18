package google

import "testing"

func TestKeyPoolRotatesAfterRateLimitBeforeSameKeyRetry(t *testing.T) {
	pool := NewKeyPool(AIStudioAPI, []KeySlot{{ID: "a", Key: "key-a"}, {ID: "b", Key: "key-b"}})
	id, key := pool.Select()
	if key == "" {
		t.Fatal("select")
	}
	nextID, nextKey := pool.Rotate(id, false)
	if nextKey == "" || nextKey == key || nextID == id {
		t.Fatalf("rotate id=%s key=%s next=%s %s", id, key, nextID, nextKey)
	}
}
