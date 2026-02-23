package github

import "testing"

func TestLiveClientImplementsClient(t *testing.T) {
	var _ Client = LiveClient{}
}
