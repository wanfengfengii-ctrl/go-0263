package store

// Memory is an in-memory Store backed by a single-connection SQLite database.
// It is used by tests and the smoke script where durability across process
// restarts is not required; the file-backed SQLite store supplies that.
type Memory struct {
	*SQLite
}

// NewMemory returns an empty in-memory Store.
func NewMemory() *Memory {
	s, err := NewSQLite(":memory:")
	if err != nil {
		panic(err)
	}
	return &Memory{SQLite: s}
}
