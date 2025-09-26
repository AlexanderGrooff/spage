package runtime

import "testing"

func TestSSHConnection_MatchesConnectionInterface(t *testing.T) {
	var _ Connection = &SSHConnection{}
}
