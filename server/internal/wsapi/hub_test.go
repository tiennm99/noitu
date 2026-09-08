package wsapi

// roomCount reports live rooms, so a test can assert that a room was evicted.
// Nothing in the server depends on it.
func (h *hub) roomCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}
