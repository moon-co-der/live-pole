package main

import "testing"

func TestIsLocalMongoURI(t *testing.T) {
	for _, raw := range []string{
		"mongodb" + "://localhost:27017/live_polling",
		"mongodb" + "://127.0.0.1:27017/live_polling",
		"mongodb" + "://[::1]:27017/live_polling",
		"mongodb" + "://mongo:27017/live_polling",
	} {
		if !isLocalMongoURI(raw) {
			t.Errorf("isLocalMongoURI(%q)=false, want true", raw)
		}
	}
	for _, raw := range []string{
		"mongodb+srv" + "://cluster.example.invalid/live_polling",
		"mongodb" + "://192.0.2.10:27017/live_polling",
		"not-a-uri",
	} {
		if isLocalMongoURI(raw) {
			t.Errorf("isLocalMongoURI(%q)=true, want false", raw)
		}
	}
}
