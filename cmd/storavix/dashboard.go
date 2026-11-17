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
	ID    string `json:"id"`
	Type  string `json:"type"`
	Wide  bool   `json:"wide"`
	MapID string `json:"mapId,omitempty"`
}

type MapNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

type MapLink struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

type TopologyMap struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Nodes []MapNode `json:"nodes"`
	Links []MapLink `json:"links"`
}

type DashboardConfig struct {
	Revision uint64        `json:"revision"`
	Widgets  []Widget      `json:"widgets"`
	Maps     []TopologyMap `json:"maps"`
}

var configID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,48}$`)

func defaultDashboard() DashboardConfig {
	return DashboardConfig{
		Widgets: []Widget{
			{ID: "raid", Type: "raid"}, {ID: "problems", Type: "problems"},
			{ID: "switches", Type: "network", Wide: true}, {ID: "map", Type: "map", Wide: true, MapID: "main"},
		},
		Maps: []TopologyMap{{ID: "main", Name: "Инфраструктура", Nodes: []MapNode{}, Links: []MapLink{}}},
	}
}

func (s *server) dashboardConfig() DashboardConfig {
	if s.state.Dashboard == nil {
		return defaultDashboard()
	}
	return *s.state.Dashboard
}

func (c DashboardConfig) validate() error {
	if c.Widgets == nil || c.Maps == nil || len(c.Widgets) > 16 || len(c.Maps) > 8 {
		return errors.New("Допустимо до 16 виджетов и до 8 карт")
	}
	maps := make(map[string]bool, len(c.Maps))
	for _, m := range c.Maps {
		if !configID.MatchString(m.ID) || maps[m.ID] || strings.TrimSpace(m.Name) == "" || len([]rune(m.Name)) > 64 ||
			len(m.Nodes) > 40 || len(m.Links) > 80 {
			return errors.New("Некорректная карта. На карте допустимо до 40 узлов и до 80 связей")
		}
		maps[m.ID] = true
		nodes := make(map[string]bool, len(m.Nodes))
		var pairs = map[[2]string]bool{}
		for _, n := range m.Nodes {
			if !configID.MatchString(n.ID) || !configID.MatchString(n.Ref) || nodes[n.ID] ||
				(n.Kind != "server" && n.Kind != "switch") || n.X < 0 || n.X > 100 || n.Y < 0 || n.Y > 100 {
				return errors.New("Некорректный узел карты")
			}
			nodes[n.ID] = true
		}
		for _, link := range m.Links {
			a, b := link.From, link.To
			if a > b {
				a, b = b, a
			}
			pair := [2]string{a, b}
			if a == b || !nodes[a] || !nodes[b] || pairs[pair] || len([]rune(link.Label)) > 48 {
				return errors.New("Некорректная или повторная связь на карте")
			}
			pairs[pair] = true
		}
	}
	widgets := map[string]bool{}
	for _, w := range c.Widgets {
		if !configID.MatchString(w.ID) || widgets[w.ID] {
			return errors.New("Некорректный виджет")
		}
		widgets[w.ID] = true
		switch w.Type {
		case "raid", "network", "problems", "events", "servers":
			if w.MapID != "" {
				return errors.New("Карта доступна в виджете топологии")
			}
		case "map":
			if !maps[w.MapID] {
				return errors.New("Выберите существующую карту")
			}
		default:
			return errors.New("Неизвестный тип виджета")
		}
	}
	return nil
}

func (s *server) getDashboardConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	config := s.dashboardConfig()
	s.mu.Unlock()
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
	HostID string `json:"hostId"`
	Host   string `json:"host"`
	Name   string `json:"name"`
	Level  string `json:"level"`
	Status string `json:"status"`
}

type DashboardProblem struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type DashboardData struct {
	Hosts    []DashboardHost    `json:"hosts"`
	Arrays   []DashboardArray   `json:"arrays"`
	Network  []NetworkDevice    `json:"network"`
	Problems []DashboardProblem `json:"problems"`
	Events   []Alert            `json:"events"`
	Drives   int                `json:"drives"`
}

func (s *server) getDashboardData(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	s.mu.Lock()
	data := DashboardData{Network: s.networkDevicesLocked(false), Events: append([]Alert{}, s.state.Alerts...)}
	for _, r := range s.agents {
		offline := now.Sub(r.ReceivedAt) > 120*time.Second
		h := DashboardHost{ID: r.ID, Name: r.Hostname, Status: "up"}
		if offline {
			h.Status = "down"
			data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, "Агент не отвечает", "critical"})
		}
		for _, a := range r.Arrays {
			status := a.Status
			if offline {
				status = "stale"
			}
			data.Arrays = append(data.Arrays, DashboardArray{r.ID, r.Hostname, a.Name, a.Level, status})
			if status == "inactive" || status == "degraded" || status == "rebuilding" {
				severity := "warning"
				if status == "inactive" {
					severity = "critical"
				}
				data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, fmt.Sprintf("%s · %s", a.Name, alarmStorageLabel(status)), severity})
				h.Status = "warning"
			}
		}
		if r.Storage != nil {
			data.Drives += len(r.Storage.Drives)
			for _, d := range r.Storage.Drives {
				if offline || (d.Status != "degraded" && d.Status != "inactive") {
					continue
				}
				name := strings.TrimSpace(d.Slot + " " + d.Device)
				if name == "" {
					name = d.Model
				}
				data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, fmt.Sprintf("Диск %s · %s", name, alarmStorageLabel(d.Status)), "warning"})
				h.Status = "warning"
			}
			for _, c := range r.Storage.Controllers {
				if !offline && (c.Status == "inactive" || c.Status == "degraded") {
					data.Problems = append(data.Problems, DashboardProblem{"server", r.ID, r.Hostname, fmt.Sprintf("%s · %s", c.Model, alarmStorageLabel(c.Status)), "warning"})
					h.Status = "warning"
				}
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
