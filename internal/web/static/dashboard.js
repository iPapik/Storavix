'use strict';

const widgetTypes = {
  raid: ['Состояние RAID', 'drives', 'storage'],
  network: ['Коммутаторы', 'network', 'network'],
  map: ['Схема сети', 'network', 'maps'],
  geomap: ['Геокарта оборудования', 'network', 'geomap'],
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
let mapDraft;
let networkPending = -1;
let networkDevices = [];
let networkEditing = '';
let mapDrag = false;
let selectedMap = '';

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
  monitoringPending = epoch;
  try {
    const [data, config] = await Promise.all([
      api('/api/dashboard'),
      !dashboardDraft && !mapDraft ? api('/api/dashboard/config') : null
    ]);
    for (const key of Object.keys(emptyMonitoring)) monitoring[key] = data[key] ?? emptyMonitoring[key];
    if (config && (!configuration || config.revision >= configuration.revision)) configuration = config;
    switch (currentRoute.page) {
      case 'overview':
        if (refreshEditors || !dashboardDraft) renderOverview();
        if ($('dashboard-msg').classList.contains('error') && !dashboardDraft) msg('dashboard-msg');
        break;
      case 'geomap':
        renderGeoPage();
        break;
      case 'maps':
        if (!mapDrag && (refreshEditors || !mapDraft)) renderMaps();
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
    if (currentRoute.page === 'maps') msg('map-msg', error.message, true);
    if (currentRoute.page === 'geomap') msg('geo-msg', error.message, true);
    if (currentRoute.page === 'alerts') $('active-alerts').replaceChildren(emptyNote(error.message));
  } finally {
    if (monitoringPending === epoch) monitoringPending = -1;
    $('dashboard-edit').disabled = !configuration;
  }
}

function resetDashboardSession() {
  monitoring = {...emptyMonitoring};
  configuration = dashboardDraft = mapDraft = undefined;
  networkDevices = [];
  networkEditing = selectedMap = geoDevice = geoRouteTarget = '';
  for (const view of geoMaps.values()) view.dispose();
  geoMaps.clear();
  for (const id of ['widget-grid', 'overview-stats', 'map-canvas', 'map-edit-list', 'geo-page-map', 'geo-locations',
    'geo-results', 'active-alerts', 'alerts-list', 'network-devices']) $(id).replaceChildren();
  $('geo-form').reset();
}


function action(label, handler, className = 'text-button') {
  const button = makeText('button', label, className);
  button.type = 'button';
  button.addEventListener('click', handler);
  return button;
}

function renderOverview() {
  const stats = [
    ['Серверы онлайн', monitoring.hosts.filter(h => h.status !== 'down').length + ' / ' + monitoring.hosts.length, 'servers'],
    ...(hasNetworkAccess() ? [['Коммутаторы', monitoring.network.length, 'network']] : []),
    ['Активные алармы', monitoring.problems.length, 'bell']
  ];
  $('overview-stats').classList.toggle('stats-servers-only', !hasNetworkAccess());
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
  $('dashboard-auto-label').classList.toggle('hidden', !editing);
  $('dashboard-auto').checked = !config.fixedLayout;
  const mapSelect = $('widget-map');
  const selected = mapSelect.value;
  updateOptions(mapSelect, config.maps.map(map => [map.id, map.name]), selected);
  updateWidgetForm();

  const grid = $('widget-grid');
  grid.replaceChildren();
  grid.classList.toggle('widget-grid-editing', editing);
  clearRemovedGeoWidgets(config.widgets);
  if (!config.widgets.length) grid.append(emptyNote('На обзоре пока нет виджетов.'));
  const positions = layoutWidgets(config.widgets, !editing && !config.fixedLayout);
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
    if (!editing) header.append(routeLink('Открыть →', page, widget.mapId));
    else {
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
    renderWidget(body, widget, config);
    grid.append(panel);
  }
}

function renderWidget(panel, widget, config) {
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
    case 'map': {
      const map = config.maps.find(entry => entry.id === widget.mapId);
      panel.append(makeText('p', map.name, 'muted widget-map-name'), buildMap(map));
      break;
    }
  }
}

function updateWidgetForm() {
  const isMap = $('widget-type').value === 'map';
  $('widget-map-label').classList.toggle('hidden', !isMap);
  $('widget-add').disabled = !!dashboardDraft && (dashboardDraft.widgets.length >= 16 || (isMap && !$('widget-map').value));
}

async function saveConfig(config) {
  configuration = await api('/api/dashboard/config', {method: 'PUT', body: JSON.stringify(config)});
}

$('dashboard-edit').addEventListener('click', () => {
  if (!configuration || !canEditDashboard()) return;
  const retained = mapDraft && !confirm('Отменить несохранённые изменения карты?');
  if (retained) return;
  mapDraft = null;
  dashboardDraft = cloneJSON(configuration);
  applyWidgetLayout(dashboardDraft.widgets, layoutWidgets(dashboardDraft.widgets, !dashboardDraft.fixedLayout));
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
$('dashboard-auto').addEventListener('change', () => {
  if (!dashboardDraft) return;
  dashboardDraft.fixedLayout = !$('dashboard-auto').checked;
  settleWidgetDraft();
  renderOverview();
});
$('widget-type').addEventListener('change', updateWidgetForm);
$('widget-map').addEventListener('change', updateWidgetForm);
$('widget-form').addEventListener('submit', event => {
  event.preventDefault();
  if (!dashboardDraft || $('widget-add').disabled) return;
  const type = $('widget-type').value;
  const widget = {id: newConfigID(), type, wide: type === 'map' || type === 'geomap'};
  if (type === 'map') widget.mapId = $('widget-map').value;
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

function buildMap(map, editable = false) {
  if (!map.nodes?.length) return emptyNote(editable ? 'Добавьте серверы и коммутаторы на карту.' : 'На этой карте пока нет узлов.');
  const canvas = document.createElement('div');
  canvas.className = 'topology' + (editable ? ' topology-editable' : '');
  canvas.setAttribute('aria-label', map.name);
  const targets = new Map(mapTargets().map(target => [target.kind + ':' + target.id, target]));
  const points = new Map(map.nodes.map(node => [node.id, node]));
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  svg.setAttribute('viewBox', '0 0 1000 500');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('class', 'topology-lines');
  const edges = [];
  const locate = node => [92 + node.x * 8.16, 58 + node.y * 3.84];
  for (const link of map.links || []) {
    const from = points.get(link.from), to = points.get(link.to);
    if (!from || !to) continue;
    const line = document.createElementNS(ns, 'line');
    const a = targets.get(from.kind + ':' + from.ref), b = targets.get(to.kind + ':' + to.ref);
    line.setAttribute('class', a?.status === 'down' || b?.status === 'down' ? 'edge-down' : 'edge');
    svg.append(line);
    const label = link.label ? makeText('span', link.label, 'topology-label') : null;
    edges.push({from, to, line, label});
  }
  canvas.append(svg);
  for (const edge of edges) if (edge.label) canvas.append(edge.label);
  const place = (element, x, y) => {
    element.style.left = x / 10 + '%';
    element.style.top = y / 5 + '%';
  };
  const drawEdges = () => {
    for (const {from, to, line, label} of edges) {
      const [x1, y1] = locate(from), [x2, y2] = locate(to);
      for (const [key, value] of Object.entries({x1, y1, x2, y2})) line.setAttribute(key, value);
      if (label) place(label, (x1 + x2) / 2, (y1 + y2) / 2);
    }
  };
  drawEdges();
  for (const node of map.nodes) {
    const target = targets.get(node.kind + ':' + node.ref);
    const status = target?.status || 'unknown';
    const title = target?.name || 'Устройство удалено';
    const state = deviceStates[status] || deviceStates.unknown;
    const anchor = makeText(editable || !target ? 'span' : 'a', '', 'topology-node ' + status);
    if (!editable && target) anchor.href = '#' + (node.kind === 'server' ? 'server' : 'network') + '/' + node.ref;
    if (editable) anchor.title = 'Перетащить узел';
    else anchor.title = title + ' · ' + state[0];
    anchor.append(makeIcon(node.kind === 'server' ? 'servers' : 'network'), makeText('strong', title),
      makeText('small', state[0]));
    canvas.append(anchor);
    place(anchor, ...locate(node));
    if (editable) {
      anchor.addEventListener('pointerdown', event => {
        if (event.button !== 0 || mapDrag) return;
        event.preventDefault();
        anchor.setPointerCapture(event.pointerId);
        mapDrag = true;
      });
      anchor.addEventListener('pointermove', event => {
        if (!anchor.hasPointerCapture(event.pointerId)) return;
        const bounds = canvas.getBoundingClientRect();
        node.x = Math.round(Math.max(0, Math.min(100, ((event.clientX - bounds.left) / bounds.width - .092) / .00816)));
        node.y = Math.round(Math.max(0, Math.min(100, ((event.clientY - bounds.top) / bounds.height - .116) / .00768)));
        place(anchor, ...locate(node));
        drawEdges();
      });
      anchor.addEventListener('lostpointercapture', () => {
        mapDrag = false;
        if (currentRoute.page === 'maps') renderMaps(false);
      });
    }
  }
  const scroller = document.createElement('div');
  scroller.className = 'topology-scroll';
  scroller.append(canvas);
  return scroller;
}

function renderMaps(redraw = true) {
  if (!configuration) return;
  const maps = configuration.maps;
  const active = maps.find(m => m.id === currentRoute.id) || maps.find(m => m.id === selectedMap) || maps[0];
  selectedMap = active?.id || '';
  updateOptions($('map-select'), maps.map(map => [map.id, map.name]), selectedMap);
  $('map-select').disabled = !!mapDraft || !maps.length;
  $('map-new').classList.toggle('hidden', !canEditDashboard());
  $('map-edit').classList.toggle('hidden', !canEditDashboard());
  $('map-new').disabled = !!mapDraft || maps.length >= 8;
  $('map-edit').disabled = !!mapDraft || !active;
  $('map-editor').classList.toggle('hidden', !mapDraft);
  $('map-edit-list').classList.toggle('hidden', !mapDraft);
  const map = mapDraft || active;
  if (redraw) $('map-canvas').replaceChildren(map ? buildMap(map, !!mapDraft) : emptyNote('Создайте первую карту сети.'));
  if (!mapDraft) return;
  $('map-delete').classList.toggle('hidden', !maps.some(m => m.id === map.id));
  const targets = mapTargets();
  const current = new Set(map.nodes.map(node => node.kind + ':' + node.ref));
  const choices = targets.filter(target => !current.has(target.kind + ':' + target.id));
  updateOptions($('map-target'), choices.map(target => [
    target.kind + ':' + target.id, target.name + (target.kind === 'server' ? ' · сервер' : ' · коммутатор')
  ]), $('map-target').value);
  $('map-node-add').disabled = !choices.length || map.nodes.length >= 40;
  const label = node => targets.find(t => t.kind === node.kind && t.id === node.ref)?.name || 'Удалённое устройство';
  const nodes = map.nodes.map(node => [node.id, label(node)]);
  updateOptions($('map-from'), nodes, $('map-from').value);
  const destination = $('map-to').value;
  updateOptions($('map-to'), nodes, destination && destination !== $('map-from').value
    ? destination : nodes.find(([id]) => id !== $('map-from').value)?.[0]);
  $('map-link-add').disabled = map.nodes.length < 2 || map.links.length >= 80;
  const inventory = $('map-edit-list');
  inventory.replaceChildren();
  if (map.nodes.length) {
    const panel = makeSection('Узлы карты');
    const rows = map.nodes.map(node => {
      const coordinate = axis => {
        const input = document.createElement('input');
        input.type = 'number';
        input.min = 0;
        input.max = 100;
        input.value = node[axis];
        input.className = 'map-coordinate';
        input.setAttribute('aria-label', axis.toUpperCase() + ' · ' + label(node));
        input.addEventListener('change', () => {
          node[axis] = Math.max(0, Math.min(100, Math.round(Number(input.value) || 0)));
          renderMaps();
        });
        return input;
      };
      return [label(node), coordinate('x'), coordinate('y'), action('Удалить', () => {
        map.nodes = map.nodes.filter(n => n.id !== node.id);
        map.links = map.links.filter(link => link.from !== node.id && link.to !== node.id);
        renderMaps();
      })];
    });
    panel.append(dataTable(['Устройство', 'X, %', 'Y, %', ''], rows));
    inventory.append(panel);
  }
  if (map.links.length) {
    const panel = makeSection('Связи');
    const labels = new Map(nodes);
    panel.append(dataTable(['От', 'До', 'Подпись', ''], map.links.map((link, index) => [
      labels.get(link.from), labels.get(link.to), link.label || '—', action('Удалить', () => {
        map.links.splice(index, 1);
        renderMaps();
      })
    ])));
    inventory.append(panel);
  }
}

function startMapEditing(map) {
  if (!canEditDashboard()) return;
  if (dashboardDraft) {
    if (!confirm('Отменить несохранённые изменения обзора?')) return;
    dashboardDraft = null;
  }
  mapDraft = map ? cloneJSON(map) : {id: newConfigID(), name: 'Новая карта', nodes: [], links: []};
  mapDraft.nodes ||= [];
  mapDraft.links ||= [];
  $('map-name').value = mapDraft.name;
  msg('map-msg');
  renderMaps();
  $('map-name').focus();
}

$('map-select').addEventListener('change', () => goto('maps', $('map-select').value));
$('map-new').addEventListener('click', () => startMapEditing());
$('map-edit').addEventListener('click', () => {
  const map = configuration?.maps.find(m => m.id === selectedMap);
  if (map) startMapEditing(map);
});
$('map-cancel').addEventListener('click', () => {
  mapDraft = null;
  msg('map-msg');
  renderMaps();
  loadMonitoring();
});
$('map-node-form').addEventListener('submit', event => {
  event.preventDefault();
  if (!mapDraft || $('map-node-add').disabled) return;
  const [kind, ref] = $('map-target').value.split(':');
  if (!ref) return;
  let i = 0;
  while (i < 40 && mapDraft.nodes.some(node => node.x === (i % 5) * 25 && node.y === Math.floor(i / 5) * 14)) i++;
  mapDraft.nodes.push({id: newConfigID(), kind, ref, x: (i % 5) * 25, y: Math.min(100, Math.floor(i / 5) * 14)});
  renderMaps();
});
$('map-link-form').addEventListener('submit', event => {
  event.preventDefault();
  if (!mapDraft || $('map-link-add').disabled) return;
  const from = $('map-from').value, to = $('map-to').value;
  if (from === to || mapDraft.links.some(link => (link.from === from && link.to === to) ||
      (link.from === to && link.to === from))) {
    msg('map-msg', 'Выберите два разных узла без существующей связи.', true);
    return;
  }
  mapDraft.links.push({from, to, label: $('map-link-label').value.trim()});
  $('map-link-label').value = '';
  msg('map-msg');
  renderMaps();
});
$('map-save-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!mapDraft) return;
  mapDraft.name = $('map-name').value.trim();
  if (!mapDraft.name) return;
  const config = cloneJSON(configuration);
  const index = config.maps.findIndex(m => m.id === mapDraft.id);
  if (index < 0) config.maps.push(mapDraft);
  else config.maps[index] = mapDraft;
  const id = mapDraft.id;
  await commitMap(config, id);
});
$('map-delete').addEventListener('click', async () => {
  if (!mapDraft || !confirm('Удалить карту и её виджеты с обзора?')) return;
  const config = cloneJSON(configuration);
  config.maps = config.maps.filter(map => map.id !== mapDraft.id);
  config.widgets = config.widgets.filter(widget => widget.mapId !== mapDraft.id);
  await commitMap(config, config.maps[0]?.id || '');
});
async function commitMap(config, id) {
  $('map-save').disabled = $('map-delete').disabled = true;
  try {
    await saveConfig(config);
    mapDraft = null;
    selectedMap = id;
    msg('map-msg', 'Изменения сохранены.');
    if (currentRoute.page === 'maps') goto('maps', id);
  } catch (error) {
    msg('map-msg', error.message, true);
  } finally {
    $('map-save').disabled = $('map-delete').disabled = false;
  }
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
    controls.append(routeLink('Местоположение', 'geomap', device.id));
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
  if (page === 'overview' || page === 'maps' || page === 'geomap' || page === 'alerts') loadMonitoring(false);
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
