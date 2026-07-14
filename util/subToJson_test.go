package util

import "testing"

func TestParseLocalSubTrimsJSON(t *testing.T) {
	got, err := ParseLocalSub("  \n{\"outbounds\":[{\"type\":\"socks\",\"tag\":\"node\"}]}\n  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["tag"] != "node" {
		t.Fatalf("unexpected outbounds: %#v", got)
	}
}
