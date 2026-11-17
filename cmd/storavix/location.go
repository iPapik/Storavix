package main

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type DeviceLocation struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address,omitempty"`
}

func (p DeviceLocation) valid() bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lon) && p.Lat >= -90 && p.Lat <= 90 &&
		p.Lon >= -180 && p.Lon <= 180 && len([]rune(p.Address)) <= 300
}

func (s *server) saveLocation(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	key := kind + ":" + id
	var in struct {
		Lat     *float64 `json:"lat"`
		Lon     *float64 `json:"lon"`
		Address string   `json:"address"`
	}
	var p DeviceLocation
	if r.Method == http.MethodPut {
		if decodeJSON(r, &in) != nil || in.Lat == nil || in.Lon == nil {
			problem(w, http.StatusBadRequest, "Укажите широту и долготу")
			return
		}
		p = DeviceLocation{*in.Lat, *in.Lon, strings.TrimSpace(in.Address)}
		if !p.valid() {
			problem(w, http.StatusBadRequest, "Проверьте диапазон координат и длину адреса")
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locationDeviceExists(kind, id) {
		problem(w, http.StatusNotFound, "Устройство не найдено")
		return
	}
	previous := s.state.Locations
	next := make(map[string]DeviceLocation, len(previous)+1)
	for existing, location := range previous {
		oldKind, oldID, _ := strings.Cut(existing, ":")
		if existing != key && s.locationDeviceExists(oldKind, oldID) {
			next[existing] = location
		}
	}
	if r.Method == http.MethodPut {
		next[key] = p
	}
	s.state.Locations = next
	if err := s.saveLocked(); err != nil {
		s.state.Locations = previous
		problem(w, http.StatusInternalServerError, "Не удалось сохранить местоположение")
		return
	}
	sendJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *server) locationDeviceExists(kind, id string) bool {
	switch kind {
	case "server":
		_, ok := s.agents[id]
		return ok
	case "switch":
		for _, d := range s.state.Network {
			if d.ID == id {
				return true
			}
		}
	}
	return false
}

type cachedLocations struct {
	points  []DeviceLocation
	expires time.Time
}

type geoSearch struct {
	mu    sync.Mutex
	busy  bool
	last  time.Time
	cache map[string]cachedLocations
}

var errGeocodingBusy = errors.New("geocoding busy")

func (s *server) findLocation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Address string `json:"address"`
	}
	if decodeJSON(r, &in) != nil {
		problem(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	address := strings.Join(strings.Fields(in.Address), " ")
	if n := len([]rune(address)); n < 3 || n > 300 {
		problem(w, http.StatusBadRequest, "Введите адрес длиной от 3 до 300 символов")
		return
	}
	points, err := s.geocoder.lookup(r, address)
	switch {
	case errors.Is(err, errGeocodingBusy):
		problem(w, http.StatusTooManyRequests, "Повторите поиск через несколько секунд")
	case err != nil:
		problem(w, http.StatusBadGateway, "Геопоиск временно недоступен. Можно ввести координаты вручную.")
	default:
		sendJSON(w, http.StatusOK, points)
	}
}

func (g *geoSearch) lookup(r *http.Request, address string) ([]DeviceLocation, error) {
	key := strings.ToLower(address)
	now := time.Now()
	g.mu.Lock()
	if cached, ok := g.cache[key]; ok && now.Before(cached.expires) {
		g.mu.Unlock()
		return cached.points, nil
	}
	if g.busy || now.Sub(g.last) < 2*time.Second {
		g.mu.Unlock()
		return nil, errGeocodingBusy
	}
	g.busy, g.last = true, now
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.busy = false
		g.mu.Unlock()
	}()
	endpoint, err := url.Parse(env("STORAVIX_GEOCODER_URL", "https://photon.komoot.io/api/"))
	if err != nil || endpoint.Host == "" || endpoint.Scheme != "https" && endpoint.Scheme != "http" {
		return nil, errors.New("invalid geocoder address")
	}
	query := endpoint.Query()
	query.Set("q", address)
	query.Set("limit", "5")
	// Photon defaults to locally available names when no language is requested.
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Storavix/"+version+" (+https://github.com/iPapik/Storavix)")
	request.Header.Set("Accept", "application/json")
	client := http.Client{Timeout: 7 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("geocoder HTTP error")
	}
	// Photon properties also include numbers and may contain nested metadata.
	var raw struct {
		Features []struct {
			Geometry struct {
				Type        string
				Coordinates []float64
			}
			Properties map[string]json.RawMessage
		}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&raw); err != nil {
		return nil, err
	}
	points := make([]DeviceLocation, 0, 5)
	for _, f := range raw.Features {
		if f.Geometry.Type != "Point" || len(f.Geometry.Coordinates) != 2 {
			continue
		}
		fields := make([]string, 0, 9)
		last := ""
		for _, property := range []string{"name", "street", "housenumber", "district", "city", "county", "state", "postcode", "country"} {
			var value string
			_ = json.Unmarshal(f.Properties[property], &value)
			value = strings.TrimSpace(value)
			if value != "" && value != last {
				fields = append(fields, value)
				last = value
			}
		}
		p := DeviceLocation{Lat: f.Geometry.Coordinates[1], Lon: f.Geometry.Coordinates[0], Address: strings.Join(fields, ", ")}
		if p.Address == "" {
			p.Address = address
		}
		if p.valid() {
			points = append(points, p)
			if len(points) == 5 {
				break
			}
		}
	}
	g.mu.Lock()
	if g.cache == nil || len(g.cache) >= 128 {
		g.cache = make(map[string]cachedLocations)
	}
	g.cache[key] = cachedLocations{points: points, expires: time.Now().Add(24 * time.Hour)}
	g.mu.Unlock()
	return points, nil
}
