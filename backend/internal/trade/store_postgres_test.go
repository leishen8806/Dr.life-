package trade

import "testing"

func TestPostgresStoreImplementsStore(t *testing.T) {
	var _ Store = (*PostgresStore)(nil)
	var _ EventStore = (*PostgresStore)(nil)
}
