package main

import (
	"strings"
	"testing"
)

// On Cloud Run a missing setting must stop the start, not fall back to the
// memory store, a random session key or plain HTTP.
func TestCloudRunRefusesToStartWithoutProductionSettings(t *testing.T) {
	prod := map[string]string{
		"K_SERVICE":                 "aestheticlab-income",
		"INCOME_FIRESTORE_PROJECT":  "project",
		"INCOME_FIRESTORE_DATABASE": "aestheticlab-income",
		"INCOME_SESSION_KEY":        strings.Repeat("k", 48),
		"INCOME_BASE_URL":           "https://kassa.example.com",
		"INCOME_START_MONTH":        "2026-10",
	}
	env := func(over map[string]string) func(string) string {
		return func(k string) string {
			if v, ok := over[k]; ok {
				return v
			}
			return prod[k]
		}
	}
	if err := checkCloudRun(env(nil)); err != nil {
		t.Fatalf("production settings refused: %v", err)
	}
	for name, over := range map[string]map[string]string{
		"no Firestore project":  {"INCOME_FIRESTORE_PROJECT": ""},
		"no Firestore database": {"INCOME_FIRESTORE_DATABASE": ""},
		"emulator":              {"FIRESTORE_EMULATOR_HOST": "localhost:8086"},
		"no session key":        {"INCOME_SESSION_KEY": ""},
		"no base URL":           {"INCOME_BASE_URL": ""},
		"http base URL":         {"INCOME_BASE_URL": "http://kassa.example.com"},
		"no start month":        {"INCOME_START_MONTH": ""},
		"bad start month":       {"INCOME_START_MONTH": "October"},
	} {
		if err := checkCloudRun(env(over)); err == nil {
			t.Errorf("%s: started anyway", name)
		}
	}
	// Local runs keep their fallbacks.
	if err := checkCloudRun(env(map[string]string{"K_SERVICE": "", "INCOME_FIRESTORE_PROJECT": "", "INCOME_BASE_URL": ""})); err != nil {
		t.Errorf("local run refused: %v", err)
	}
}
