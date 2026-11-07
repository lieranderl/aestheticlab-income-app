package bookings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	calendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/lieranderl/aestheticlab-income-app/internal/domain"
)

// Google reads calendars through the Calendar API with read-only scope. In
// production the Cloud Run service account's own identity is used; each
// staff calendar is shared with it («See all event details»).
type Google struct{ svc *calendar.Service }

// NewGoogle connects with Application Default Credentials. opts are for tests.
func NewGoogle(ctx context.Context, opts ...option.ClientOption) (*Google, error) {
	opts = append([]option.ClientOption{option.WithScopes(calendar.CalendarReadonlyScope)}, opts...)
	svc, err := calendar.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("calendar client: %w", err)
	}
	return &Google{svc: svc}, nil
}

// List follows every page; if any page fails the whole read fails.
func (g *Google) List(ctx context.Context, cal string, from, to time.Time) ([]Event, error) {
	var out []Event
	call := g.svc.Events.List(cal).Context(ctx).
		TimeMin(from.Format(time.RFC3339)).TimeMax(to.Format(time.RFC3339)).
		SingleEvents(true).OrderBy("startTime").MaxResults(250).
		Fields("nextPageToken", "items(id,status,summary,description,start)")
	for page := ""; ; {
		res, err := call.PageToken(page).Do()
		if err != nil {
			return nil, err
		}
		for _, it := range res.Items {
			out = append(out, eventOf(it))
		}
		if page = res.NextPageToken; page == "" {
			return out, nil
		}
	}
}

// Get returns one event; 404 and 410 mean it is gone.
func (g *Google) Get(ctx context.Context, cal, id string) (Event, bool, error) {
	it, err := g.svc.Events.Get(cal, id).Context(ctx).Fields("id", "status", "summary", "description", "start").Do()
	if err != nil {
		var ge *googleapi.Error
		if errors.As(err, &ge) && (ge.Code == http.StatusNotFound || ge.Code == http.StatusGone) {
			return Event{}, false, nil
		}
		return Event{}, false, err
	}
	return eventOf(it), true, nil
}

func eventOf(it *calendar.Event) Event {
	ev := Event{ID: it.Id, Summary: it.Summary, Description: it.Description, Cancelled: it.Status == "cancelled"}
	if it.Start != nil && it.Start.DateTime != "" {
		if t, err := time.Parse(time.RFC3339, it.Start.DateTime); err == nil {
			ev.Start = t
		}
	} else if it.Start != nil && it.Start.Date != "" {
		if d, err := domain.ParseDate(it.Start.Date); err == nil {
			ev.AllDay = d
		}
	}
	return ev
}

// Warm makes one cheap call so the credentials token and the connection to
// Google exist before the first screen asks for bookings. After a scale-to-
// zero start, the first real read otherwise spends most of Timeout on them.
func (g *Google) Warm(ctx context.Context) error {
	_, err := g.svc.CalendarList.List().Context(ctx).MaxResults(1).Fields("items(id)").Do()
	return err
}
