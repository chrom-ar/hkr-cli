package channel

import "testing"

func TestSocketURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:4001":        "ws://localhost:4001/api/v1/socket/websocket?vsn=2.0.0",
		"https://hkr.example.com/":     "wss://hkr.example.com/api/v1/socket/websocket?vsn=2.0.0",
		"https://example.com/hkr":      "wss://example.com/hkr/api/v1/socket/websocket?vsn=2.0.0",
		"https://example.com/?x=1#top": "wss://example.com/api/v1/socket/websocket?vsn=2.0.0",
		"wss://example.com":            "wss://example.com/api/v1/socket/websocket?vsn=2.0.0",
	} {
		got, err := SocketURL(in)
		if err != nil || got != want {
			t.Errorf("SocketURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"ftp://example.com", "example.com", "https://", "://"} {
		if got, err := SocketURL(in); err == nil {
			t.Errorf("SocketURL(%q) = %q, want an error", in, got)
		}
	}
}
