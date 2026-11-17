package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeoSearchAndCache(t *testing.T) {
	calls := 0
	photon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.Header.Get("User-Agent"), "Storavix/") || r.URL.Query().Get("q") != "Test address" {
			t.Errorf("unexpected geocoder request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"features":[{"geometry":{"type":"Point","coordinates":[73.4,61.25]},"properties":{"name":"Test address","city":"Test city","osm_id":123}}]}`))
	}))
	defer photon.Close()
	t.Setenv("STORAVIX_GEOCODER_URL", photon.URL)
	g := geoSearch{}
	for i := 0; i < 2; i++ {
		query := httptest.NewRequest("POST", "/", nil)
		locations, err := g.lookup(query, "Test address")
		if err != nil || len(locations) != 1 || locations[0].Lat != 61.25 || locations[0].Lon != 73.4 ||
			locations[0].Address != "Test address, Test city" {
			t.Fatalf("unexpected address match: %+v %v", locations, err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected cached answer, upstream calls: %d", calls)
	}
}

func TestWidgetGridCoordinates(t *testing.T) {
	config := defaultDashboard()
	config.Widgets[0].Layout = &WidgetLayout{X: 0, Y: 0, W: 7, H: 3}
	config.Widgets[1].Layout = &WidgetLayout{X: 6, Y: 0, W: 6, H: 2}
	if config.validate() == nil {
		t.Fatal("overlap accepted")
	}
	config.Widgets[1].Layout.X = 7
	if config.validate() == nil {
		t.Fatal("overflow accepted")
	}
	config.Widgets[1].Layout.W = 5
	config.Widgets = append(config.Widgets, Widget{ID: "geo", Type: "geomap", Layout: &WidgetLayout{X: 0, Y: 4, W: 12, H: 6}})
	if err := config.validate(); err != nil {
		t.Fatal(err)
	}
}
