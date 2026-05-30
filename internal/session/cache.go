package session

import (
	"crypto/rand"
	"sync"
)

// Cache stores per-host credentials with XOR obfuscation against a one-time
// session key so credentials are not held as plaintext strings on the heap.
type Cache struct {
	mu      sync.RWMutex
	key     []byte
	entries map[string]entry
}

type entry struct {
	user []byte
	pass []byte
}

func NewCache() *Cache {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &Cache{
		key:     key,
		entries: make(map[string]entry),
	}
}

func xorBytes(data, key []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}

func (c *Cache) Set(host, username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[host] = entry{
		user: xorBytes([]byte(username), c.key),
		pass: xorBytes([]byte(password), c.key),
	}
}

func (c *Cache) Get(host string) (username, password string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, found := c.entries[host]
	if !found {
		return "", "", false
	}
	return string(xorBytes(e.user, c.key)), string(xorBytes(e.pass, c.key)), true
}

func (c *Cache) Delete(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, host)
}
