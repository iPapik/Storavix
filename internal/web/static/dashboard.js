'use strict';

const widgetTypes = {
  raid: ['Состояние RAID', 'drives', 'servers'],
  network: ['Сетевое оборудование', 'network', 'network'],
  geomap: ['Геокарта оборудования', 'network', ''],
  problems: ['Активные алармы', 'bell', 'alerts'],
  events: ['Журнал уведомлений', 'logs', 'alerts'],
  servers: ['Состояние серверов', 'servers', 'servers']
};
const deviceStates = {
  up: ['Доступен', 'green'], critical: ['Критическое', 'storage-failed'], down: ['Нет связи', 'storage-failed'],
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
let networkLayout = 'large';
try {const stored=localStorage.getItem('storavix.network.layout');if(systemsLayoutModes.includes(stored))networkLayout=stored;}catch(_){}
$('network-layout').value=networkLayout;

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
  info.append(routeLink(problem.source, problem.kind === 'server' ? 'server' : 'device', problem.ref),
    makeText('small', problem.message, 'muted'));
  if (problem.occurredAt) {
    const date = makeText('time', new Date(problem.occurredAt).toLocaleString('ru-RU'), 'muted problem-date');
    date.dateTime = problem.occurredAt;
    info.append(date);
  }
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
    ...(hasServerAccess() ? [['Серверы онлайн', monitoring.hosts.filter(h => h.status !== 'down').length + ' / ' + monitoring.hosts.length, 'servers', 'servers']] : []),
    ...(hasNetworkAccess() ? [['Маршрутизаторы и коммутаторы', monitoring.network.length, 'network', 'network']] : []),
    ['Активные алармы', monitoring.problems.length, 'bell', 'alerts']
  ];
  $('overview-stats').classList.toggle('stats-servers-only', stats.length === 2);
  $('overview-stats').replaceChildren(...stats.map(([name, value, icon, page]) => {
    const tile = routeLink('', page);
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
        row.append(info, array.memberIssue ? storageSummaryTag('active', array.status, 'диск') : healthTag(array.status));
        return row;
      }, 'Ожидание данных о массивах.');
      break;
    }
    case 'network':
      addList(panel, monitoring.network.slice(0, 8), device => {
        const row = document.createElement('div');
        row.className = 'widget-list-row';
        const info = document.createElement('div');
        info.append(routeLink(device.name, 'device', device.id), makeText('small',
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
    if (currentRoute.page === 'device') renderNetworkDetail();
  } catch (error) {
    if (epoch === sessionEpoch) msg('network-msg', error.message, true);
  } finally {
    if (networkPending === epoch) networkPending = -1;
  }
}

function equipmentType(d){return d.kind==='router'?'Маршрутизатор':'Коммутатор'}
function equipmentLocation(d){return !d.location?'Не указано':(d.location.address||d.location.lat.toFixed(5)+', '+d.location.lon.toFixed(5))}
function fmtLinkSpeed(value){return !value?'—':value>=1000?(value/1000).toLocaleString('ru-RU',{maximumFractionDigits:2})+' Гбит/с':value+' Мбит/с'}
function networkStatus(d){return d.status==='up'?['online','Онлайн']:d.status==='down'?['offline','Нет связи']:d.status==='stale'?['warn','Данные устарели']:['warn','Ожидание опроса']}
function snmpStatus(d){return d.method==='snmp'?(d.status==='up'?'Работает':'Настроен, нет ответа'):'Не настроен'}
function syslogStatus(d){return d.syslogLastAt?'Получены: '+new Date(d.syslogLastAt).toLocaleString('ru-RU'):d.syslogExpected?'Ожидаются сообщения':'Не указан'}
function makeNetworkCard(d){
  const [state,label]=networkStatus(d),card=document.createElement('a');
  card.className='system-card network-system-card';card.href='#device/'+d.id;card.setAttribute('aria-label','Подробнее об оборудовании '+d.name);
  const header=document.createElement('div');header.className='system-head';
  header.append(makeText('i','','system-dot '+state),makeText('b',d.name),makeText('span',label,'system-state state-text '+state));
  const body=document.createElement('div');body.className='system-metrics';
  const speed=Math.max(0,...(d.ports||[]).filter(p=>p.status==='up').map(p=>p.speedMbps||0));
  body.append(
    metricRow('network','Тип:',equipmentType(d)),metricRow('network','IP / DNS:',d.host),
    metricRow('services','Порты:',d.portCount?d.upPorts+' / '+d.portCount:'—'),
    metricRow('activity','Скорость:',fmtLinkSpeed(speed)),
    metricRow('network','Трафик:',d.status==='up'?fmtRate(d.totalRate):'—'),
    metricRow('drive','Приём:',d.status==='up'?fmtRate(d.rxRate):'—'),
    metricRow('drive','Передача:',d.status==='up'?fmtRate(d.txRate):'—'),
    metricRow('radio','SNMP:',snmpStatus(d)),metricRow('logs','Syslog:',syslogStatus(d)),
    metricRow('clock','Работает:',d.status==='up'?fmtUptime(d.uptimeSeconds):'—'),
    metricRow('network','Место:',equipmentLocation(d))
  );
  card.append(header,body);return card;
}
function renderNetwork(){
  const root=$('network-devices'),query=$('network-filter').value.trim().toLowerCase();
  const devices=networkDevices.filter(d=>[d.name,d.host,d.kind,d.system,d.location?.address].some(v=>typeof v==='string'&&v.toLowerCase().includes(query)));
  root.classList.remove('large-layout','standard-layout','compact-layout','list-layout');root.classList.add(networkLayout+'-layout');
  root.replaceChildren(...(devices.length?devices.map(makeNetworkCard):[emptyNote(networkDevices.length?'По фильтру ничего не найдено.':'Добавьте маршрутизатор или коммутатор для мониторинга.')]));
}
function renderNetworkDetail(){
  if(currentRoute.page!=='device')return;
  const root=$('network-detail'),d=networkDevices.find(device=>device.id===currentRoute.id);
  if(!d){root.replaceChildren(emptyNote('Устройство не найдено.'));return;}
  setPageTitle(d.name);
  const [state,label]=networkStatus(d),section=makeSection('Состояние оборудования');
  const tiles=document.createElement('div');tiles.className='server-tiles';
  for(const [k,v] of [
    ['Состояние',label],['Активных портов',d.portCount?d.upPorts+' / '+d.portCount:'—'],
    ['Входящий трафик',d.status==='up'?fmtRate(d.rxRate):'—'],
    ['Исходящий трафик',d.status==='up'?fmtRate(d.txRate):'—']
  ]){const tile=document.createElement('div');tile.append(makeText('span',k,'muted'),makeText('strong',v));tiles.append(tile);}
  section.append(tiles);
  for(const [k,v] of [
    ['Тип',equipmentType(d)],['Имя оборудования',d.name],['IP / DNS',d.host],
    ['SNMP sysName',d.system||'—'],['Состояние',label],['Метод опроса',d.method==='snmp'?'SNMP v2c':'TCP-порт'],
    ['SNMP',snmpStatus(d)],['Syslog',syslogStatus(d)],
    ['Время работы',d.status==='up'?fmtUptime(d.uptimeSeconds):'—'],
    ['Температура',d.temperature!=null&&d.status==='up'?fmtTemperature(d.temperature):'—'],
    ['Последний опрос',d.checkedAt?new Date(d.checkedAt).toLocaleString('ru-RU'):'—'],
    ['Местоположение',equipmentLocation(d)],
    ['Координаты',d.location?d.location.lat+', '+d.location.lon:'—'],
    ['Общий трафик',d.status==='up'?fmtRate(d.totalRate):'—']
  ])detailRow(section,k,v,k==='Состояние'?'state-text '+state:'');
  if(d.message)section.append(makeText('p',d.message,'muted'));
  const actions=document.createElement('div');actions.className='toolbar-actions network-detail-actions';
  if(d.method==='snmp')actions.append(routeLink('Журнал SNMP','snmp-events',d.id));
  if(hasFullAccess())actions.append(routeLink('Журнал Syslog','syslog'));
  if(canManageDevices())actions.append(action('Изменить',()=>{goto('network');editNetwork(d)},'secondary small'),
    action('Удалить',()=>removeNetwork(d),'secondary small'));
  section.append(actions);
  const ports=makeSection('Порты и скорость интерфейсов');
  if(d.ports?.length){
    ports.append(dataTable(['Порт','Состояние','Скорость линка','Приём','Передача'],
      d.ports.map(p=>[p.name,statusTag(p.status),fmtLinkSpeed(p.speedMbps),
        d.status==='up'?fmtRate(p.rxRate):'—',d.status==='up'?fmtRate(p.txRate):'—'])));
  }else ports.append(emptyNote(d.method==='snmp'?'Оборудование ещё не отдало список портов по SNMP.':'При TCP-проверке невозможно собрать состояние интерфейсов. Включите SNMP.'));
  root.replaceChildren(section,ports);
}

function editNetwork(device) {
  networkEditing = device?.id || 'new';
  $('network-name').value = device?.name || '';
  $('network-kind').value = device?.kind || 'switch';
  $('network-syslog').checked = !!device?.syslogExpected;
  $('network-host').value = device?.host || '';
  $('network-method').value = device?.method || 'snmp';
  $('network-port').value = device?.port || 161;
  $('network-community').value = '';
  $('network-temp-oid').value = device?.temperatureOID || '';
  $('network-temp-divisor').value = device?.temperatureDivisor || 1;
  $('network-community').placeholder = device?.method === 'snmp' ? 'Оставьте пустым, чтобы сохранить' : '';
  $('network-form').classList.remove('hidden');
  msg('network-msg');
  syncNetworkForm();
  $('network-name').focus();
}

function syncNetworkForm() {
  const snmp = $('network-method').value === 'snmp';
  $('network-community-label').classList.toggle('hidden', !snmp);
  $('network-temp-oid-label').classList.toggle('hidden', !snmp);
  $('network-temp-divisor-label').classList.toggle('hidden', !snmp || !$('network-temp-oid').value.trim());
  const existing = networkDevices.find(device => device.id === networkEditing);
  $('network-community').required = snmp && existing?.method !== 'snmp';
}

async function removeNetwork(device) {
  if (!confirm('Удалить «' + device.name + '» из мониторинга?')) return;
  try {
    await api('/api/network/' + device.id, {method: 'DELETE'});
    if (networkEditing === device.id) $('network-form').classList.add('hidden');
    if (currentRoute.page === 'device') goto('network');
    await loadNetwork();
  } catch (error) {
    msg('network-msg', error.message, true);
  }
}

$('network-add').addEventListener('click', () => editNetwork());
$('device-refresh').addEventListener('click',loadNetwork);
$('network-filter').addEventListener('input',renderNetwork);
$('network-layout').addEventListener('change',()=>{
  networkLayout=systemsLayoutModes.includes($('network-layout').value)?$('network-layout').value:'large';
  try{localStorage.setItem('storavix.network.layout',networkLayout)}catch(_){}
  renderNetwork();
});
$('network-cancel').addEventListener('click', () => {
  $('network-form').classList.add('hidden');
  $('network-community').value = '';
  msg('network-msg');
});
$('network-method').addEventListener('change', () => {
  $('network-port').value = $('network-method').value === 'snmp' ? 161 : 443;
  syncNetworkForm();
});
$('network-temp-oid').addEventListener('input', syncNetworkForm);
$('network-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button = $('network-save');
  button.disabled = true;
  try {
    const method = $('network-method').value;
    const device = {
      name: $('network-name').value.trim(), kind:$('network-kind').value,
      syslogExpected:$('network-syslog').checked, host: $('network-host').value.trim(), method,
      port: Number($('network-port').value), community: method === 'snmp' ? $('network-community').value : '',
      temperatureOID: method === 'snmp' ? $('network-temp-oid').value.trim() : '',
      temperatureDivisor: method === 'snmp' ? Number($('network-temp-divisor').value) : 0
    };
    const result = await api('/api/network/' + networkEditing, {method: 'PUT', body: JSON.stringify(device)});
    $('network-form').classList.add('hidden');
    $('network-community').value = '';
    msg('network-msg', 'Настройки сохранены. Ожидаем первый опрос.');
    goto('device', result.id);
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
  if (page === 'servers') loadSystems();
  if (page === 'network' || page === 'device') loadNetwork();
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
