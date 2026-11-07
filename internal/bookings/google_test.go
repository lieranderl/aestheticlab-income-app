package bookings_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/option"

	"github.com/lieranderl/aestheticlab-income-app/internal/bookings"
)

func fakeCalendarAPI(t *testing.T, h http.HandlerFunc) *bookings.Google {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	g, err := bookings.NewGoogle(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication(), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestGoogleListsEveryPageOfTheDayWindow(t *testing.T) {
	var queries []string
	g := fakeCalendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if !strings.HasSuffix(r.URL.Path, "/calendars/"+mariaCal+"/events") {
			t.Errorf("path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("singleEvents") != "true" || q.Get("timeMin") != "2026-11-07T00:00:00+01:00" || q.Get("timeMax") != "2026-11-08T00:00:00+01:00" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if q.Get("pageToken") == "" {
			writeJSON(w, map[string]any{"nextPageToken": "p2", "items": []any{
				map[string]any{"id": "a", "summary": "S", "description": "D", "start": map[string]any{"dateTime": "2026-11-07T10:00:00+01:00"}},
				map[string]any{"id": "allday", "start": map[string]any{"date": "2026-11-07"}},
			}})
			return
		}
		writeJSON(w, map[string]any{"items": []any{map[string]any{"id": "b", "start": map[string]any{"dateTime": "2026-11-07T11:00:00+01:00"}}}})
	})
	from, to := mustDate(t, "2026-11-07").Window(brussels)
	events, err := g.List(context.Background(), mariaCal, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || len(events) != 3 || events[0].ID != "a" || events[2].ID != "b" {
		t.Fatalf("queries=%d events=%+v", len(queries), events)
	}
	if !events[0].Start.Equal(at("2026-11-07", "10:00")) || events[0].Summary != "S" || events[0].Description != "D" || !events[1].Start.IsZero() {
		t.Fatalf("events = %+v", events)
	}
}

func TestGoogleFailsTheWholeDayWhenALaterPageFails(t *testing.T) {
	g := fakeCalendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			writeJSON(w, map[string]any{"nextPageToken": "p2", "items": []any{map[string]any{"id": "a", "start": map[string]any{"dateTime": "2026-11-07T10:00:00+01:00"}}}})
			return
		}
		http.Error(w, `{"error":{"code":500,"message":"backend"}}`, http.StatusInternalServerError)
	})
	from, to := mustDate(t, "2026-11-07").Window(brussels)
	if events, err := g.List(context.Background(), mariaCal, from, to); err == nil {
		t.Fatalf("partial day returned: %+v", events)
	}
}

func TestGoogleTellsGoneEventsFromFailures(t *testing.T) {
	g := fakeCalendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/events/gone404"):
			http.Error(w, `{"error":{"code":404,"message":"Not Found"}}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/events/gone410"):
			http.Error(w, `{"error":{"code":410,"message":"Gone"}}`, http.StatusGone)
		case strings.HasSuffix(r.URL.Path, "/events/cancelled"):
			writeJSON(w, map[string]any{"id": "cancelled", "status": "cancelled"})
		case strings.HasSuffix(r.URL.Path, "/events/here"):
			writeJSON(w, map[string]any{"id": "here", "status": "confirmed", "start": map[string]any{"dateTime": "2026-11-07T10:00:00+01:00"}})
		default:
			http.Error(w, `{"error":{"code":503,"message":"down"}}`, http.StatusServiceUnavailable)
		}
	})
	ctx := context.Background()
	for _, id := range []string{"gone404", "gone410"} {
		if _, found, err := g.Get(ctx, mariaCal, id); found || err != nil {
			t.Errorf("%s: found=%v err=%v, want gone", id, found, err)
		}
	}
	if ev, found, err := g.Get(ctx, mariaCal, "cancelled"); !found || err != nil || !ev.Cancelled {
		t.Errorf("cancelled: %+v %v %v", ev, found, err)
	}
	if ev, found, err := g.Get(ctx, mariaCal, "here"); !found || err != nil || ev.Cancelled || !ev.Start.Equal(at("2026-11-07", "10:00")) {
		t.Errorf("here: %+v %v %v", ev, found, err)
	}
	if _, _, err := g.Get(ctx, mariaCal, "down"); err == nil {
		t.Error("503 must be an error, not a deletion")
	}
}

func TestGoogleWarmsUpItsConnectionBeforeTheFirstScreen(t *testing.T) {
	var asked []string
	g := fakeCalendarAPI(t, func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		writeJSON(w, map[string]any{"items": []any{}})
	})
	if err := g.Warm(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !strings.HasSuffix(asked[0], "/users/me/calendarList") {
		t.Fatalf("asked %v", asked)
	}
}
