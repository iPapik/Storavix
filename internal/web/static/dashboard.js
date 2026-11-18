'use strict';

const widgetTypes = {
  raid: ['Состояние RAID', 'drives', 'storage'],
  network: ['Коммутаторы', 'network', 'network'],
  geomap: ['Геокарта оборудования', 'network', ''],
  problems: ['Активные алармы', 'bell', 'alerts'],
  events: ['Журнал уведомлений', 'logs', 'alerts'],
  servers: ['Состояние серверов', 'servers', 'servers']
};
const deviceStates = {
  up: ['Доступен', 'green'], down: ['Нет связи', 'storage-failed'],
  warning: ['Требуется внимание', 'storage-warning'], stale: ['Данные устарели', 'grey'],
  pending: ['Ожидание опроса', 'grey'], disabled: ['Отключён', 'grey'], unknown: ['Неизвестно', 'grey']
};

const emptyMonitoring = {hosts: [], arrays: [], network: [], problems: [], events: [], locations: {}};
let monitoring = {...emptyMonitoring};
let configuration;
let dashboardDraft;
let monitoringPending = -1;
let networkPending = -1;
let networkDevices = [];
let networkEditing = '';

function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value));
}

function newConfigID() {
  // getRandomValues also works when the panel is accessed through a LAN HTTP origin.
  return Array.from(crypto.getRandomValues(new Uint8Array(12)), b => b.toString(16).padStart(2, '0')).join('');
}

function updateVersion(version) {
  if (!version) return;
  const display = 'v' + version;
  $('version').textContent = display;
  $('footer-version').textContent = display;
}

function statusTag(status, states = deviceStates) {
  const [label, color] = states[status] || states.unknown;
  return makeText('span', label, 'tag ' + color);
}

function routeLink(label, page, id = '') {
  const link = makeText('a', label, 'text-link');
  link.href = '#' + page + (id ? '/' + id : '');
  return link;
}

function emptyNote(label) {
  return makeText('p', label, 'muted empty-note');
}

function addList(parent, items, makeItem, emptyText) {
  if (!items.length) parent.append(emptyNote(emptyText));
  else parent.append(...items.map(makeItem));
}

function problemNode(problem) {
  const row = document.createElement('div');
  row.className = 'widget-list-row';
  const info = document.createElement('div');
  info.append(routeLink(problem.source, problem.kind === 'server' ? 'server' : 'network', problem.ref),
    makeText('small', problem.message, 'muted'));
  const critical = problem.severity === 'critical';
  row.append(info, makeText('span', critical ? 'Критично' : 'Внимание',
    'tag ' + (critical ? 'storage-failed' : 'storage-warning')));
  return row;
}

function portSummary(device) {
  if (!device.portCount) return device.method === 'tcp' ? 'TCP ' + device.port : 'SNMP v2c';
  const text = 'Интерфейсов активны ' + device.upPorts + ' из ' + device.portCount;
  return device.totalRate != null ? text + ' · Σ ' + fmtRate(device.totalRate) : text;
}

async function loadMonitoring(eventOrRefresh = true) {
  const epoch = sessionEpoch;
  if (monitoringPending === epoch) return;
  const refreshEditors = eventOrRefresh !== false;
  const revision = locationRevision;
  monitoringPending = epoch;
  try {
    const [data, config] = await Promise.all([
      api('/api/dashboard'),
      !dashboardDraft ? api('/api/dashboard/config') : null
    ]);
    for (const key of Object.keys(emptyMonitoring)) {
      if (key !== 'locations' || revision === locationRevision) monitoring[key] = data[key] ?? structuredClone(emptyMonitoring[key]);
    }
    if (config && (!configuration || config.revision >= configuration.revision)) configuration = config;
    switch (currentRoute.page) {
      case 'overview':
        if (refreshEditors || !dashboardDraft) renderOverview();
        if ($('dashboard-msg').classList.contains('error') && !dashboardDraft) msg('dashboard-msg');
        break;
      case 'alerts':
        $('active-alerts').replaceChildren();
        $('alerts-list').replaceChildren();
        addList($('active-alerts'), monitoring.problems, problemNode, 'Активных алармов нет.');
        addList($('alerts-list'), monitoring.events, eventNode, 'Журнал пока пуст.');
        break;
    }
  } catch (error) {
    if (epoch !== sessionEpoch) return;
    if (currentRoute.page === 'overview') msg('dashboard-msg', error.message, true);
    if (currentRoute.page === 'alerts') $('active-alerts').replaceChildren(emptyNote(error.message));
  } finally {
    if (monitoringPending === epoch) monitoringPending = -1;
    $('dashboard-edit').disabled = !configuration;
  }
}

function resetDashboardSession() {
  monitoring = {...emptyMonitoring};
  configuration = dashboardDraft = undefined;
  networkDevices = [];
  networkEditing = '';
  for (const view of geoMaps.values()) view.dispose();
  geoMaps.clear();
  for (const id of ['widget-grid', 'overview-stats', 'active-alerts', 'alerts-list', 'network-devices']) $(id).replaceChildren();
}


function action(label, handler, className = 'text-button') {
  const button = makeText('button', label, className);
  button.type = 'button';
  button.addEventListener('click', handler);
  return button;
}

function renderOverview() {
  const stats = [
    ...(hasServerAccess() ? [['Серверы онлайн', monitoring.hosts.filter(h => h.status !== 'down').length + ' / ' + monitoring.hosts.length, 'servers']] : []),
    ...(hasNetworkAccess() ? [['Коммутаторы', monitoring.network.length, 'network']] : []),
    ['Активные алармы', monitoring.problems.length, 'bell']
  ];
  $('overview-stats').classList.toggle('stats-servers-only', stats.length === 2);
  $('overview-stats').replaceChildren(...stats.map(([name, value, icon]) => {
    const tile = document.createElement('div');
    tile.className = 'panel overview-stat';
    const heading = document.createElement('div');
    heading.append(makeIcon(icon), makeText('span', name, 'muted'));
    tile.append(heading, makeText('strong', value));
    return tile;
  }));
  const config = dashboardDraft || configuration;
  if (!config) return;
  const editing = !!dashboardDraft;
  $('dashboard-edit').classList.toggle('hidden', editing || !canEditDashboard());
  $('dashboard-save').classList.toggle('hidden', !editing);
  $('dashboard-cancel').classList.toggle('hidden', !editing);
  $('widget-form').classList.toggle('hidden', !editing);
  updateWidgetForm();

  const grid = $('widget-grid');
  grid.replaceChildren();
  grid.classList.toggle('widget-grid-editing', editing);
  clearRemovedGeoWidgets(config.widgets);
  if (!config.widgets.length) grid.append(emptyNote('На обзоре пока нет виджетов.'));
  const positions = layoutWidgets(config.widgets, !editing);
  const ordered = [...config.widgets].filter(widget => positions.has(widget.id)).sort((a, b) => {
    const x = positions.get(a.id), y = positions.get(b.id);
    return x.y - y.y || x.x - y.x;
  });
  for (const widget of ordered) {
    const [title, icon, page] = widgetTypes[widget.type];
    const panel = document.createElement('article');
    panel.className = 'panel dashboard-widget';
    panel.dataset.widget = widget.id;
    drawWidgetPosition(panel, positions.get(widget.id));
    const header = document.createElement('header');
    const heading = makeText('h3', title);
    heading.prepend(makeIcon(icon));
    header.append(heading);
    if (!editing && page) header.append(routeLink('Открыть →', page));
    if (editing) {
      header.prepend(widgetHandle(widget, panel, false));
      header.append(action('Удалить', () => {
        dashboardDraft.widgets = dashboardDraft.widgets.filter(item => item.id !== widget.id);
        settleWidgetDraft();
        renderOverview();
      }, 'widget-remove'));
      panel.append(widgetHandle(widget, panel, true));
    }
    const body = document.createElement('div');
    body.className = 'widget-body' + (widget.type === 'geomap' ? ' widget-geo' : '');
    panel.append(header, body);
    renderWidget(body, widget);
    grid.append(panel);
  }
}

function renderWidget(panel, widget) {
  switch (widget.type) {
    case 'raid': {
      const summary = document.createElement('div');
      summary.className = 'widget-badges';
      for (const state of ['active', 'degraded', 'rebuilding', 'inactive', 'unknown', 'stale']) {
        const count = monitoring.arrays.filter(array => array.status === state).length;
        if (count) {
          const badge = healthTag(state);
          badge.append(' · ' + count);
          summary.append(badge);
        }
      }
      panel.append(summary);
      const priority = {inactive: 0, degraded: 1, rebuilding: 2, stale: 3, unknown: 4, active: 5};
      const arrays = [...monitoring.arrays].sort((a, b) => (priority[a.status] ?? 4) - (priority[b.status] ?? 4));
      addList(panel, arrays.slice(0, 8), array => {
        const row = document.createElement('div');
        row.className = 'widget-list-row';
        const info = document.createElement('div');
        info.append(routeLink(array.host, 'server', array.hostId),
          makeText('small', [array.name, array.level].filter(Boolean).join(' · '), 'muted'));
        row.append(info, healthTag(array.status));
        return row;
      }, 'Ожидание данных о массивах.');
      break;
    }
    case 'network':
      addList(panel, monitoring.network.slice(0, 8), device => {
        const row = document.createElement('div');
        row.className = 'widget-list-row';
        const info = document.createElement('div');
        info.append(routeLink(device.name, 'network', device.id), makeText('small',
          device.status === 'up' ? portSummary(device) : device.host, 'muted'));
        row.append(info, statusTag(device.status));
        return row;
      }, 'Добавьте коммутаторы в разделе «Сеть».');
      break;
    case 'problems':
      addList(panel, monitoring.problems.slice(0, 8), problemNode, 'Активных алармов нет.');
      break;
    case 'events':
      addList(panel, monitoring.events.slice(0, 6), eventNode, 'Журнал пока пуст.');
      break;
    case 'servers':
      addList(panel, monitoring.hosts.slice(0, 8), host => {
        const row = document.createElement('div');
        row.className = 'widget-list-row';
        row.append(routeLink(host.name, 'server', host.id), statusTag(host.status));
        return row;
      }, 'Ожидание отчётов агентов.');
      break;
    case 'geomap':
      panel.append(geoMap('widget-' + widget.id));
      break;

  }
}

function updateWidgetForm() {
  $('widget-add').disabled = !!dashboardDraft && dashboardDraft.widgets.length >= 16;
}

async function saveConfig(config) {
  configuration = await api('/api/dashboard/config', {method: 'PUT', body: JSON.stringify(config)});
}

$('dashboard-edit').addEventListener('click', () => {
  if (!configuration || !canEditDashboard()) return;
  dashboardDraft = cloneJSON(configuration);
  applyWidgetLayout(dashboardDraft.widgets, layoutWidgets(dashboardDraft.widgets, true));
  msg('dashboard-msg');
  renderOverview();
});
$('dashboard-cancel').addEventListener('click', () => {
  dashboardDraft = null;
  msg('dashboard-msg');
  renderOverview();
  loadMonitoring();
});
$('dashboard-save').addEventListener('click', async () => {
  if (!dashboardDraft) return;
  const button = $('dashboard-save');
  button.disabled = true;
  try {
    await saveConfig(dashboardDraft);
    dashboardDraft = null;
    msg('dashboard-msg', 'Обзор сохранён.');
    renderOverview();
  } catch (error) {
    msg('dashboard-msg', error.message, true);
  } finally {
    button.disabled = false;
  }
});
$('widget-form').addEventListener('submit', event => {
  event.preventDefault();
  if (!dashboardDraft || $('widget-add').disabled) return;
  const type = $('widget-type').value;
  const widget = {id: newConfigID(), type, wide: type === 'geomap'};
  const positions = layoutWidgets([...dashboardDraft.widgets, widget]);
  widget.layout = positions.get(widget.id);
  if (!widget.layout) { msg('dashboard-msg', 'В сетке нет свободного места для виджета.', true); return; }
  dashboardDraft.widgets.push(widget);
  settleWidgetDraft();
  renderOverview();
});

function updateOptions(select, values, preferred) {
  select.replaceChildren(...values.map(([value, label]) => new Option(label, value)));
  if (values.some(([value]) => value === preferred)) select.value = preferred;
}

function mapTargets() {
  return [
    ...monitoring.hosts.map(host => ({...host, kind: 'server'})),
    ...monitoring.network.map(device => ({...device, kind: 'switch'}))
  ];
}

async function loadNetwork() {
  const epoch = sessionEpoch;
  if (networkPending === epoch) return;
  networkPending = epoch;
  try {
    networkDevices = await api('/api/network');
    if (currentRoute.page === 'network') renderNetwork();
  } catch (error) {
    if (epoch === sessionEpoch) msg('network-msg', error.message, true);
  } finally {
    if (networkPending === epoch) networkPending = -1;
  }
}

function renderNetwork() {
  const root = $('network-devices');
  const open = new Set([...root.querySelectorAll('details[open]')].map(node => node.dataset.device));
  const selected = currentRoute.id;
  if (selected && root.dataset.selected !== selected) open.add(selected);
  root.dataset.selected = selected;
  root.replaceChildren();
  addList(root, networkDevices, device => {
    const panel = document.createElement('details');
    panel.className = 'panel network-device';
    panel.dataset.device = device.id;
    panel.open = open.has(device.id);
    const summary = document.createElement('summary');
    summary.append(makeText('strong', device.name), makeText('span', device.host, 'muted'), statusTag(device.status));
    panel.append(summary);
    const details = document.createElement('div');
    details.className = 'network-device-details';
    const proto = device.method === 'snmp' ? 'SNMP v2c · UDP ' : 'TCP ';
    details.append(makeText('p', [device.system, proto + device.port,
      device.checkedAt && ('Опрос ' + new Date(device.checkedAt).toLocaleString('ru-RU'))].filter(Boolean).join(' · '), 'muted'));
    if (device.message) details.append(makeText('p', device.message, 'muted'));
    if (device.ports?.length) {
      details.append(makeText('p', device.status === 'stale' ? 'Последний полученный снимок интерфейсов' : portSummary(device), 'muted'), dataTable(
        ['Интерфейс', 'Состояние', 'Приём', 'Передача'],
        device.ports.map(port => [port.name, statusTag(port.status),
          device.status === 'up' ? fmtRate(port.rxRate) : '—', device.status === 'up' ? fmtRate(port.txRate) : '—'])));
    }
    const controls = document.createElement('div');
    controls.className = 'toolbar-actions';
    if (device.method === 'snmp') controls.append(routeLink('События SNMP', 'snmp-events', device.id));
    if (canManageDevices()) controls.append(action('Изменить', () => editNetwork(device), 'secondary small'),
      action('Удалить', () => removeNetwork(device), 'text-button'));
    details.append(controls);
    panel.append(details);
    return panel;
  }, 'Коммутаторы пока не добавлены.');
}

function editNetwork(device) {
  networkEditing = device?.id || 'new';
  $('network-name').value = device?.name || '';
  $('network-host').value = device?.host || '';
  $('network-method').value = device?.method || 'snmp';
  $('network-port').value = device?.port || 161;
  $('network-community').value = '';
  $('network-community').placeholder = device?.method === 'snmp' ? 'Оставьте пустым, чтобы сохранить' : '';
  $('network-form').classList.remove('hidden');
  msg('network-msg');
  syncNetworkForm();
  $('network-name').focus();
}

function syncNetworkForm() {
  const snmp = $('network-method').value === 'snmp';
  $('network-community-label').classList.toggle('hidden', !snmp);
  const existing = networkDevices.find(device => device.id === networkEditing);
  $('network-community').required = snmp && existing?.method !== 'snmp';
}

async function removeNetwork(device) {
  if (!confirm('Удалить «' + device.name + '» из мониторинга?')) return;
  try {
    await api('/api/network/' + device.id, {method: 'DELETE'});
    if (networkEditing === device.id) $('network-form').classList.add('hidden');
    await loadNetwork();
  } catch (error) {
    msg('network-msg', error.message, true);
  }
}

$('network-add').addEventListener('click', () => editNetwork());
$('network-cancel').addEventListener('click', () => {
  $('network-form').classList.add('hidden');
  $('network-community').value = '';
  msg('network-msg');
});
$('network-method').addEventListener('change', () => {
  $('network-port').value = $('network-method').value === 'snmp' ? 161 : 443;
  syncNetworkForm();
});
$('network-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button = $('network-save');
  button.disabled = true;
  try {
    const method = $('network-method').value;
    const device = {
      name: $('network-name').value.trim(), host: $('network-host').value.trim(), method,
      port: Number($('network-port').value), community: method === 'snmp' ? $('network-community').value : ''
    };
    const result = await api('/api/network/' + networkEditing, {method: 'PUT', body: JSON.stringify(device)});
    $('network-form').classList.add('hidden');
    $('network-community').value = '';
    msg('network-msg', 'Настройки сохранены. Ожидаем первый опрос.');
    goto('network', result.id);
  } catch (error) {
    msg('network-msg', error.message, true);
  } finally {
    button.disabled = false;
  }
});
$('network-check').addEventListener('click', async () => {
  try {
    await api('/api/network/check', {method: 'POST'});
    msg('network-msg', 'Опрос запрошен. Панель обновится автоматически.');
    setTimeout(loadNetwork, 4500);
  } catch (error) {
    msg('network-msg', error.message, true);
  }
});

setInterval(() => {
  if (!session || document.hidden) return;
  const {page, id} = currentRoute;
  if (page === 'overview' || page === 'alerts') loadMonitoring(false);
  if (page === 'snmp-events') refreshSNMPJournal();
  if (page === 'syslog' && !syslogCursor) loadSyslogJournal(0, true);
  if (page === 'server') loadServer(id);
  if (page === 'servers' || page === 'storage') loadSystems();
  if (page === 'network') loadNetwork();
}, 15000);

(async function boot() {
  try {
    const state = await api('/api/session');
    updateVersion(state.version);
    if (state.authenticated) showShell(state.user);
    else showAuth();
  } catch (error) {
    showAuth();
    msg('login-msg', error.message, true);
  }
})();
