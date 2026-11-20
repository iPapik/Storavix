package main

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Widget struct {
	ID     string        `json:"id"`
	Type   string        `json:"type"`
	Wide   bool          `json:"wide"`
	Layout *WidgetLayout `json:"layout,omitempty"`
}

type WidgetLayout struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (p WidgetLayout) valid() bool {
	return p.X >= 0 && p.X <= 9 && p.Y >= 0 && p.Y < 192 &&
		p.W >= 3 && p.W <= 12 && p.H >= 2 && p.H <= 12 && p.X+p.W <= 12
}

func (p WidgetLayout) overlaps(other WidgetLayout) bool {
	return p.X < other.X+other.W && other.X < p.X+p.W && p.Y < other.Y+other.H && other.Y < p.Y+p.H
}

type DashboardConfig struct {
	Revision uint64   `json:"revision"`
	Widgets  []Widget `json:"widgets"`
}

var configID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,48}$`)

func defaultDashboard() DashboardConfig {
	return DashboardConfig{
		Widgets: []Widget{
			{ID: "raid", Type: "raid"}, {ID: "problems", Type: "problems"},
			{ID: "switches", Type: "network", Wide: true}, {ID: "geomap", Type: "geomap", Wide: true},
		},
	}
}

func (s *server) dashboardConfig() DashboardConfig {
	if s.state.Dashboard == nil {
		return defaultDashboard()
	}
	return *s.state.Dashboard
}

func (c DashboardConfig) validate() error {
	if c.Widgets == nil || len(c.Widgets) > 16 {
		return errors.New("Допустимо до 16 виджетов")
	}
	widgets := map[string]bool{}
	var positions []WidgetLayout
	for _, w := range c.Widgets {
		if !configID.MatchString(w.ID) || widgets[w.ID] {
			return errors.New("Некорректный виджет")
		}
		widgets[w.ID] = true
		if w.Layout != nil {
			if !w.Layout.valid() {
				return errors.New("Виджет выходит за пределы сетки")
			}
			for _, position := range positions {
				if w.Layout.overlaps(position) {
					return errors.New("Виджеты не могут перекрываться")
				}
			}
			positions = append(positions, *w.Layout)
		}
		switch w.Type {
		case "raid", "network", "problems", "events", "servers", "geomap":
		default:
			return errors.New("Неизвестный тип виджета")
		}
	}
	return nil
}

func (s *server) getDashboardConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	config := s.dashboardConfig()
	s.mu.Unlock()
	user := requestAccount(r)
	widgets := make([]Widget, 0, len(config.Widgets))
	for _, widget := range config.Widgets {
		switch widget.Type {
		case "map": // Removed network topology widgets from older dashboards.
			continue
		case "raid", "servers":
			if !user.servers() {
				continue
			}
		case "network":
			if !user.network() {
				continue
			}
		case "events":
			if !user.full() {
				continue
			}
		}
		widgets = append(widgets, widget)
	}
	config.Widgets = widgets

	sendJSON(w, http.StatusOK, config)
}

func (s *server) putDashboardConfig(w http.ResponseWriter, r *http.Request) {
	var config DashboardConfig
	if decodeJSONLimit(r, &config, 512*1024) != nil {
		problem(w, http.StatusBadRequest, "Не удалось прочитать настройки обзора")
		return
	}
	if err := config.validate(); err != nil {
		problem(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if config.Revision != s.dashboardConfig().Revision {
		problem(w, http.StatusConflict, "Настройки изменились в другом окне. Обновите данные перед повторным сохранением.")
		return
	}
	config.Revision++
	previous := s.state.Dashboard
	s.state.Dashboard = &config
	if err := s.saveLocked(); err != nil {
		s.state.Dashboard = previous
		problem(w, http.StatusInternalServerError, "Не удалось сохранить обзор")
		return
	}
	sendJSON(w, http.StatusOK, config)
}

type DashboardHost struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type DashboardArray struct {
	HostID      string `json:"hostId"`
	Host        string `json:"host"`
	Name        string `json:"name"`
	Level       string `json:"level"`
	Status      string `json:"status"`
	MemberIssue bool   `json:"memberIssue,omitempty"`
}

type DashboardProblem struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type DashboardData struct {
	Hosts     []DashboardHost           `json:"hosts"`
	Arrays    []DashboardArray          `json:"arrays"`
	Network   []NetworkDevice           `json:"network"`
	Problems  []DashboardProblem        `json:"problems"`
	Events    []Alert                   `json:"events"`
	Drives    int                       `json:"drives"`
	Locations map[string]DeviceLocation `json:"locations"`
}

func (s *server) getDashboardData(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	user := requestAccount(r)
	s.mu.Lock()
	data := DashboardData{Locations: make(map[string]DeviceLocation, len(s.state.Locations))}
	if user.network() {
		data.Network = s.networkDevicesLocked(false)
	}
	if user.full() {
		data.Events = append([]Alert{}, s.state.Alerts...)
	}
	for key, pos := range s.state.Locations {
		kind, id, _ := strings.Cut(key, ":")
		if (kind == "switch" && user.network() || kind == "server" && user.servers()) && s.locationDeviceExists(kind, id) {
			data.Locations[key] = pos
		}
	}
	for _, r := range s.agents {
		if !user.servers() {
			break
		}
		offline := now.Sub(r.ReceivedAt) > 120*time.Second
		h := DashboardHost{ID: r.ID, Name: r.Hostname, Status: "up"}
		if offline {
			h.Status = "down"
			data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, "Агент не отвечает", "critical"})
		}
		issue := "active"
		addProblem := func(name, status string) {
			rank := storageIssueRank(status)
			if offline || rank == 0 {
				return
			}
			if rank > storageIssueRank(issue) {
				issue = status
			}
			severity := "warning"
			if status == "inactive" {
				severity = "critical"
			}
			data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, fmt.Sprintf("%s · %s", name, alarmStorageLabel(status)), severity})
		}
		drives := make(map[string]string)
		if r.Storage != nil {
			data.Drives += len(r.Storage.Drives)
			for _, d := range r.Storage.Drives {
				drives[d.ID] = d.Status
				name := strings.TrimSpace(d.Slot + " " + d.Device)
				if name == "" {
					name = d.Model
				}
				addProblem("Диск "+name, d.Status)
			}
			for _, c := range r.Storage.Controllers {
				addProblem(c.Model, c.Status)
			}
			for _, source := range r.Storage.Sources {
				if storageIssueRank(issue) == 0 && (source.Status == "error" || source.Status == "partial") {
					issue = "degraded"
				}
			}
		}
		for _, a := range r.Arrays {
			status, memberIssue := a.Status, false
			if offline {
				status = "stale"
			} else {
				for _, id := range a.Members {
					if s := drives[id]; storageIssueRank(s) > storageIssueRank(status) {
						status, memberIssue = s, true
					}
				}
			}
			data.Arrays = append(data.Arrays, DashboardArray{r.ID, r.Hostname, a.Name, a.Level, status, memberIssue})
			addProblem(a.Name, a.Status)
		}
		if !offline {
			if issue == "inactive" {
				h.Status = "critical"
			} else if storageIssueRank(issue) != 0 {
				h.Status = "warning"
			}
		}
		data.Hosts = append(data.Hosts, h)
	}
	s.mu.Unlock()
	sort.Slice(data.Hosts, func(i, j int) bool {
		return data.Hosts[i].Name+data.Hosts[i].ID < data.Hosts[j].Name+data.Hosts[j].ID
	})
	sort.Slice(data.Arrays, func(i, j int) bool {
		a, b := data.Arrays[i], data.Arrays[j]
		return a.Host+a.HostID+a.Name < b.Host+b.HostID+b.Name
	})
	for _, n := range data.Network {
		if n.Status == "down" {
			data.Problems = append(data.Problems, DashboardProblem{"switch", n.ID, n.Name, "Коммутатор не отвечает на настроенную проверку", "critical"})
		}
	}
	sort.Slice(data.Problems, func(i, j int) bool {
		a, b := data.Problems[i], data.Problems[j]
		return a.Severity+a.Source+a.Message < b.Severity+b.Source+b.Message
	})
	sendJSON(w, http.StatusOK, data)
}

func storageIssueRank(status string) int {
	switch status {
	case "inactive":
		return 3
	case "degraded":
		return 2
	case "rebuilding":
		return 1
	default:
		return 0
	}
}

func alarmStorageLabel(status string) string {
	switch status {
	case "inactive":
		return "недоступен"
	case "rebuilding":
		return "восстановление"
	default:
		return "деградация"
	}
}

func (s *server) getAgent(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	report, ok := s.agents[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		problem(w, http.StatusNotFound, "Сервер не найден")
		return
	}
	sendJSON(w, http.StatusOK, report)
}
