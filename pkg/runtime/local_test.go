package runtime

import "testing"

func TestLocalConnection_MatchesConnectionInterface(t *testing.T) {
	var _ Connection = &LocalConnection{}
}
