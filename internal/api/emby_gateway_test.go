package api

import "testing"

func TestEmbyDirectItemID(t *testing.T) {
	tests := []struct {
		path string
		id   string
		ok   bool
	}{
		{path: "/Videos/163515/stream", id: "163515", ok: true},
		{path: "/videos/163515/stream.mkv", id: "163515", ok: true},
		{path: "/emby/Videos/163515/original.mkv", id: "163515", ok: true},
		{path: "/emby/videos/abc/original", id: "abc", ok: true},
		{path: "/emby/videos/163515/main.m3u8", ok: false},
		{path: "/hls1/main/0.ts", ok: false},
		{path: "/api/health", ok: false},
	}

	for _, tt := range tests {
		id, ok := embyDirectItemID(tt.path)
		if ok != tt.ok || id != tt.id {
			t.Fatalf("embyDirectItemID(%q) = (%q, %v), want (%q, %v)", tt.path, id, ok, tt.id, tt.ok)
		}
	}
}

func TestEmbyBaseURLCandidatesFromRequestHost(t *testing.T) {
	candidates, err := embyBaseURLCandidates("http://127.0.0.1:8096", "10.0.0.158:8097")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) < 2 {
		t.Fatalf("expected loopback fallback candidates, got %d", len(candidates))
	}
	if got, want := candidates[0].String(), "http://127.0.0.1:8096"; got != want {
		t.Fatalf("first candidate = %q, want %q", got, want)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.String() == "http://10.0.0.158:8096" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing same-host fallback in %v", candidates)
	}
}
