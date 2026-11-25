package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/telemetry"
)

const (
	// Do not treat a couple of missed heartbeats as a server outage.
	agentStaleAfter   = 2 * time.Minute
	agentOfflineAfter = 10 * time.Minute
	eventHistoryLimit = 1000
	mailQueueLimit    = 500
)

type alertRule struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Severity string `json:"-"`
}

var alertRules = []alertRule{
	{"server_down", "Сервер недоступен", "Серверы", "critical"},
	{"server_up", "Сервер снова на связи", "Серверы", "info"},
	{"server_hot", "Высокая температура сервера", "Серверы", "warning"},
	{"server_cooled", "Температура сервера в норме", "Серверы", "info"},
	{"disk_space_low", "Мало свободного места на системном томе", "Серверы", "warning"},
	{"disk_space_ok", "Свободное место на системном томе восстановлено", "Серверы", "info"},
	{"switch_down", "Коммутатор недоступен", "Коммутаторы", "critical"},
	{"switch_up", "Коммутатор снова доступен", "Коммутаторы", "info"},
	{"switch_hot", "Высокая температура коммутатора", "Коммутаторы", "warning"},
	{"switch_cooled", "Температура коммутатора в норме", "Коммутаторы", "info"},
	{"raid_rebuild_started", "Началось восстановление RAID", "Хранилище", "warning"},
	{"raid_rebuild_finished", "Восстановление RAID завершено", "Хранилище", "info"},
	{"raid_degraded", "RAID в состоянии деградации", "Хранилище", "warning"},
	{"raid_critical", "Критический отказ RAID или контроллера", "Хранилище", "critical"},
	{"raid_recovered", "RAID снова в норме", "Хранилище", "info"},
	{"raid_disk_warning", "Предупреждение о состоянии физического диска", "Хранилище", "warning"},
	{"raid_disk_failed", "Физический диск недоступен", "Хранилище", "critical"},
}

var alertRuleByID = func() map[string]alertRule {
	rules := make(map[string]alertRule, len(alertRules))
	for _, rule := range alertRules {
		rules[rule.ID] = rule
	}
	return rules
}()

func validateAlertSelection(c MailConfig) error {
	seen := make(map[string]bool, len(c.Alerts))
	for _, id := range c.Alerts {
		if _, ok := alertRuleByID[id]; !ok || seen[id] {
			return errors.New("Выберите события из списка SMTP")
		}
		seen[id] = true
	}
	for _, limit := range []int{c.ServerTempLimit, c.SwitchTempLimit} {
		if limit != 0 && (limit < 10 || limit > 125) {
			return errors.New("Порог температуры должен быть от 10 до 125 °C")
		}
	}
	return nil
}

func serverTempLimit(c MailConfig) int {
	if c.ServerTempLimit == 0 {
		return 80
	}
	return c.ServerTempLimit
}

func switchTempLimit(c MailConfig) int {
	if c.SwitchTempLimit == 0 {
		return 70
	}
	return c.SwitchTempLimit
}

type monitorState struct {
	Generation string                      `json:"generation,omitempty"`
	Conditions map[string]monitorCondition `json:"conditions"`
}

func (m monitorState) since(key, status string) *time.Time {
	condition := m.Conditions[key]
	if condition.Status == status && !condition.Since.IsZero() {
		return &condition.Since
	}
	return nil
}

type monitorCondition struct {
	Status string    `json:"status"`
	Since  time.Time `json:"since"`
}

type monitorChange struct {
	Key, Status, Event, Detail string
	ReportFirst                bool
}

func arrayMonitorKey(a telemetry.Array) string {
	return "array:" + a.Controller + "\x1f" + a.Group + "\x1f" + a.Name
}

func (s *server) applyMonitorLocked(key, generation, source string, at time.Time, changes []monitorChange) {
	old, exists := s.state.Monitor[key]
	previous := old
	if old.Generation != generation {
		old = monitorState{Generation: generation}
	}
	next := old
	var events []Alert
	pending := -1
	telegramPending := -1
	changed := false
	for _, observation := range changes {
		prior, known := old.Conditions[observation.Key]
		if known && observation.Status == prior.Status {
			continue
		}
		if !changed {
			next.Conditions = make(map[string]monitorCondition, len(old.Conditions)+len(changes))
			maps.Copy(next.Conditions, old.Conditions)
			changed = true
		}
		next.Conditions[observation.Key] = monitorCondition{observation.Status, at}
		rule, ok := alertRuleByID[observation.Event]
		if !ok || (!known && !observation.ReportFirst) {
			continue
		}
		delivery := "disabled"
		deliveryError := ""
		if slices.Contains(s.state.Mail.Alerts, rule.ID) {
			if pending == -1 {
				pending = 0
				for _, event := range s.state.Alerts {
					if event.Delivery == "queued" || event.Delivery == "sending" {
						pending++
					}
				}
			}
			if pending < mailQueueLimit {
				delivery = "queued"
				pending++
			} else {
				delivery = "failed"
				deliveryError = "Очередь SMTP переполнена"
			}
		}
		telegramDelivery, telegramError := "disabled", ""
		if s.state.Telegram.Enabled && slices.Contains(s.state.Telegram.Alerts, rule.ID) {
			if telegramPending == -1 {
				telegramPending = 0
				for _, event := range s.state.Alerts {
					if pendingTelegram(event) { telegramPending++ }
				}
			}
			if telegramPending < mailQueueLimit {
				telegramDelivery = "queued"
				telegramPending++
			} else {
				telegramDelivery, telegramError = "failed", "Очередь Telegram переполнена"
			}
		}
		events = append(events, Alert{ID: hex.EncodeToString(randomBytes(8)), CreatedAt: at, Severity: rule.Severity,
			Subject: rule.Label + " — " + source, Detail: observation.Detail, Kind: rule.ID, Delivery: delivery, DeliveryError: deliveryError, TelegramDelivery: telegramDelivery, TelegramDeliveryError: telegramError})
	}
	if !changed {
		return
	}
	// Retain missing components through partial polls. Bound the history of
	// components replaced over the lifetime of a device.
	if len(next.Conditions) > 2*(telemetry.MaxArrays+telemetry.MaxDrives+telemetry.MaxControllers+4) {
		current := make(map[string]monitorCondition, len(changes))
		for _, observation := range changes {
			current[observation.Key] = next.Conditions[observation.Key]
		}
		next.Conditions = current
	}
	if s.state.Monitor == nil {
		s.state.Monitor = make(map[string]monitorState)
	}
	s.state.Monitor[key] = next
	oldEvents := s.state.Alerts
	if len(events) > 0 {
		s.state.Alerts = trimAlerts(append(events, oldEvents...))
	}
	if err := s.saveLocked(); err != nil {
		if exists {
			s.state.Monitor[key] = previous
		} else {
			delete(s.state.Monitor, key)
		}
		s.state.Alerts = oldEvents
		log.Printf("record monitoring event: %v", err)
	}
}

func trimAlerts(events []Alert) []Alert {
	slices.SortStableFunc(events, func(a, b Alert) int { return b.CreatedAt.Compare(a.CreatedAt) })
	// Preserve pending deliveries for either channel when trimming history.
	if len(events) <= eventHistoryLimit {
		return events
	}
	result := make([]Alert, 0, eventHistoryLimit)
	pending := 0
	for _, event := range events {
		if event.Delivery == "queued" || event.Delivery == "sending" || pendingTelegram(event) {
			pending++
		}
	}
	for _, event := range events {
		isPending := event.Delivery == "queued" || event.Delivery == "sending" || pendingTelegram(event)
		if isPending {
			pending--
		}
		if isPending || len(result)+pending < eventHistoryLimit {
			result = append(result, event)
		}
	}
	return result
}

func limitChange(key, detail string, value float64, limit, hysteresis int, previous, above, below string) monitorChange {
	status, event := "normal", below
	if value >= float64(limit) || previous == "high" && value > float64(limit-hysteresis) {
		status, event = "high", above
	}
	return monitorChange{key, status, event, detail, status == "high"}
}

func (s *server) observeAgentLocked(r telemetry.Report) {
	key := "server:" + r.ID
	prior := s.state.Monitor[key].Conditions
	availabilityDetail := "Получен отчёт агента. Мониторинг телеметрии восстановлен."
	if previous := prior["availability"]; previous.Status == "down" && !previous.Since.IsZero() {
		availabilityDetail = fmt.Sprintf("Получен отчёт агента. Продолжительность подтверждённой потери телеметрии: %s. %s",
			r.ReceivedAt.Sub(previous.Since).Round(time.Second), reportIncidentSummary(r))
	}
	changes := []monitorChange{{"availability", "up", "server_up", availabilityDetail, false}}
	if r.Temperature != nil {
		limit := serverTempLimit(s.state.Mail)
		detail := fmt.Sprintf("Температура %.1f °C · порог %d °C.", *r.Temperature, limit)
		changes = append(changes, limitChange("temperature", detail, *r.Temperature, limit, 5,
			prior["temperature"].Status, "server_hot", "server_cooled"))
	}
	if r.DiskPercent != nil {
		detail := fmt.Sprintf("Системный том занят на %.1f%%.", *r.DiskPercent)
		changes = append(changes, limitChange("disk-space", detail, *r.DiskPercent, 90, 5,
			prior["disk-space"].Status, "disk_space_low", "disk_space_ok"))
	}
	freshStorage := r.Storage == nil || r.Storage.CollectedAt.IsZero() ||
		r.ReceivedAt.Sub(r.Storage.CollectedAt) <= 4*time.Minute
	if freshStorage {
		for _, a := range r.Arrays {
			id := arrayMonitorKey(a)
			var event string
			switch a.Status {
			case "rebuilding":
				event = "raid_rebuild_started"
			case "degraded":
				event = "raid_degraded"
			case "inactive":
				event = "raid_critical"
			case "active":
				event = "raid_recovered"
				if prior[id].Status == "rebuilding" {
					event = "raid_rebuild_finished"
				}
			default:
				continue
			}
			detail := strings.Trim(strings.Join([]string{a.Name, a.Level, a.Detail}, " · "), " ·")
			changes = append(changes, monitorChange{id, a.Status, event, detail, a.Status != "active"})
		}
		if r.Storage != nil {
			for _, c := range r.Storage.Controllers {
				if c.Status != "active" && c.Status != "degraded" && c.Status != "inactive" {
					continue
				}
				event := map[string]string{"inactive": "raid_critical", "degraded": "raid_degraded", "active": "raid_recovered"}[c.Status]
				changes = append(changes, monitorChange{"controller:" + c.ID, c.Status, event, c.Model, c.Status != "active"})
			}
			for _, d := range r.Storage.Drives {
				if d.Status != "active" && d.Status != "degraded" && d.Status != "inactive" {
					continue
				}
				event := map[string]string{"degraded": "raid_disk_warning", "inactive": "raid_disk_failed"}[d.Status]
				detail := strings.Trim(strings.Join([]string{d.Slot, d.Device, d.Model, d.Detail}, " · "), " ·")
				changes = append(changes, monitorChange{"drive:" + d.ID, d.Status, event, detail, event != ""})
			}
		}
	}
	s.applyMonitorLocked(key, "", r.Hostname, r.ReceivedAt, changes)
}

func (s *server) observeNetworkLocked(n NetworkConfig, sample networkSample) {
	var changes []monitorChange
	switch sample.status {
	case "up":
		changes = append(changes, monitorChange{"availability", "up", "switch_up", "Коммутатор ответил на проверку.", false})
	case "down":
		changes = append(changes, monitorChange{"availability", "down", "switch_down", sample.message, true})
	}
	key := "switch:" + n.ID
	if sample.status == "up" && sample.temperature != nil {
		limit := switchTempLimit(s.state.Mail)
		prior := s.state.Monitor[key]
		status := ""
		if prior.Generation == n.Generation {
			status = prior.Conditions["temperature"].Status
		}
		detail := fmt.Sprintf("Температура %.1f °C · порог %d °C.", *sample.temperature, limit)
		changes = append(changes, limitChange("temperature", detail, *sample.temperature, limit, 5, status, "switch_hot", "switch_cooled"))
	}
	s.applyMonitorLocked(key, n.Generation, n.Name, sample.at, changes)
}

// reportIncidentSummary enriches email notifications using the last accepted
// report, without assuming old metrics still describe the current server.
func reportIncidentSummary(r telemetry.Report) string {
	items := []string{}
	if r.OSName != "" { items = append(items, "ОС: "+r.OSName) }
	if r.Version != "" { items = append(items, "Агент: "+r.Version) }
	if r.UptimeSeconds != nil { items = append(items, fmt.Sprintf("Время работы ОС на момент последнего отчёта: %s", (time.Duration(*r.UptimeSeconds)*time.Second).Round(time.Minute))) }
	if r.CPUPercent != nil { items = append(items, fmt.Sprintf("ЦП на момент последнего отчёта: %.1f%%", *r.CPUPercent)) }
	if r.MemoryPercent != nil { items = append(items, fmt.Sprintf("Память на момент последнего отчёта: %.1f%%", *r.MemoryPercent)) }
	if r.DiskPercent != nil { items = append(items, fmt.Sprintf("Системный том на момент последнего отчёта: %.1f%%", *r.DiskPercent)) }
	if r.Diagnostics != nil {
		d := r.Diagnostics
		if d.MetricsStatus != "" { items=append(items, "Сбор метрик: "+d.MetricsStatus) }
		if d.SampleAgeSeconds != nil { items=append(items, fmt.Sprintf("Возраст метрик: %d с",*d.SampleAgeSeconds)) }
		if d.SendFailures > 0 { items=append(items,fmt.Sprintf("Предыдущие ошибки доставки: %d",d.SendFailures)) }
	}
	if len(items)==0 {return "Дополнительная телеметрия отсутствует."}
	return strings.Join(items, ". ")+"."
}

func (s *server) scanOffline(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.agents {
		if now.Sub(r.ReceivedAt) > agentOfflineAfter {
			s.applyMonitorLocked("server:"+r.ID, "", r.Hostname, now.UTC(), []monitorChange{
				{"availability", "down", "server_down",
					fmt.Sprintf("Подтверждена потеря телеметрии: новых отчётов нет %s (порог %s). Последний успешно полученный отчёт: %s UTC. %s Причину следует искать в службе агента, сети и журнале Windows; само выключение ОС этим событием не подтверждается.",
						now.Sub(r.ReceivedAt).Round(time.Second), agentOfflineAfter,
						r.ReceivedAt.UTC().Format("02.01.2006 15:04:05"), reportIncidentSummary(r)), true},
			})
		}
	}
}

func (s *server) runOfflineMonitor(ctx context.Context) {
	// Allow already enrolled agents to reconnect after a server restart.
	startup := time.NewTimer(agentOfflineAfter)
	defer startup.Stop()
	select {
	case <-ctx.Done():
		return
	case <-startup.C:
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.scanOffline(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *server) updateDeliveryLocked(index int, delivery string, err error) bool {
	alert := &s.state.Alerts[index]
	previous := *alert
	alert.Delivery = delivery
	alert.Delivered = delivery == "sent"
	alert.DeliveryError = ""
	if err != nil {
		alert.DeliveryError = safeErr(err)
	}
	if err := s.saveLocked(); err != nil {
		*alert = previous
		log.Printf("record mail delivery: %v", err)
		return false
	}
	return true
}

func (s *server) deliverMonitorMail(ctx context.Context) bool {
	s.mu.Lock()
	index := -1
	for i, alert := range s.state.Alerts {
		if alert.Delivery == "queued" && (index < 0 || alert.CreatedAt.Before(s.state.Alerts[index].CreatedAt)) {
			index = i
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return false
	}
	event, cfg, encrypted := s.state.Alerts[index], s.state.Mail, s.state.EncryptedSecret
	if !slices.Contains(cfg.Alerts, event.Kind) {
		saved := s.updateDeliveryLocked(index, "disabled", nil)
		s.mu.Unlock()
		return saved
	}
	if !s.updateDeliveryLocked(index, "sending", nil) {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	secret, err := s.decrypt(encrypted)
	if err == nil {
		body := fmt.Sprintf("%s\n%s\nВремя события: %s UTC", event.Subject, event.Detail, event.CreatedAt.UTC().Format("02.01.2006 15:04:05"))
		err = sendMailContext(ctx, cfg, secret, event.Subject, body)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, alert := range s.state.Alerts {
		if alert.ID == event.ID {
			if err != nil {
				return s.updateDeliveryLocked(i, "failed", err)
			}
			return s.updateDeliveryLocked(i, "sent", nil)
		}
	}
	return false
}

func (s *server) runMonitorMail(ctx context.Context) {
	s.mu.Lock()
	for i, alert := range s.state.Alerts {
		if alert.Delivery == "sending" {
			s.updateDeliveryLocked(i, "unknown", errors.New("Служба остановилась до подтверждения отправки"))
		}
	}
	s.mu.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if s.deliverMonitorMail(ctx) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
