package cache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/honeycombio/honeycomb-grafana-plugin/pkg/cache"
)

func TestCache_SetAndGet(t *testing.T) {
	c := cache.New(1 * time.Minute)
	defer c.Stop()

	c.Set("key", "value", 10*time.Second)

	v, ok := c.Get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if v.(string) != "value" {
		t.Errorf("expected 'value', got %v", v)
	}
}

func TestCache_Expiry(t *testing.T) {
	c := cache.New(50 * time.Millisecond)
	defer c.Stop()

	c.Set("key", "value", 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	_, ok := c.Get("key")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

func TestCache_Delete(t *testing.T) {
	c := cache.New(1 * time.Minute)
	defer c.Stop()

	c.Set("key", "value", 1*time.Hour)
	c.Delete("key")

	_, ok := c.Get("key")
	if ok {
		t.Error("expected cache miss after delete")
	}
}

func TestCache_MissingKey(t *testing.T) {
	c := cache.New(1 * time.Minute)
	defer c.Stop()

	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := cache.New(1 * time.Minute)
	defer c.Stop()

	const workers = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			key := "key"
			c.Set(key, i, 10*time.Second)
			c.Get(key)
		}()
	}
	wg.Wait()
}

func TestSingleflightGroup_CollapsesConcurrentCalls(t *testing.T) {
	var g cache.Group
	var callCount int
	var mu sync.Mutex

	fn := func() (interface{}, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return "result", nil
	}

	const workers = 10
	var wg sync.WaitGroup
	results := make([]interface{}, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			v, _, _ := g.Do("same-key", fn)
			results[i] = v
		}()
	}
	wg.Wait()

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got > 2 {
		// Allow up to 2 because goroutines may not all arrive before fn returns.
		t.Errorf("expected 1-2 actual function calls, got %d", got)
	}
	for i, r := range results {
		if r.(string) != "result" {
			t.Errorf("worker %d got wrong result: %v", i, r)
		}
	}
}

func TestSingleflightGroup_DifferentKeysAreIndependent(t *testing.T) {
	var g cache.Group
	var callCount int
	var mu sync.Mutex

	fn := func() (interface{}, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return "result", nil
	}

	g.Do("key-a", fn)
	g.Do("key-b", fn)

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got != 2 {
		t.Errorf("expected 2 calls for different keys, got %d", got)
	}
}
