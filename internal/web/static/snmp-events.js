'use strict';

let snmpFilters = {};
let snmpCursor = 0;
let snmpNext = 0;
let snmpRequest = 0;

function resetSNMPJournal() {
  snmpFilters = {};
  snmpCursor = snmpNext = 0;
  $('snmp-event-filter').reset();
  $('snmp-event-rows').replaceChildren();
  $('snmp-traps-form').reset();
  $('snmp-traps-form').classList.add('hidden');
  $('snmp-traps-toggle').setAttribute('aria-expanded', 'false');
  msg('snmp-events-msg');
  msg('snmp-traps-msg');
}

async function openSNMPJournal(reset, id) {
  $('snmp-traps-toggle').classList.toggle('hidden', !isAdmin());
  if (reset) {
    snmpCursor = 0;
    snmpFilters = {device: id};
    $('snmp-event-kind').value = $('snmp-event-query').value = '';
    $('snmp-event-device').value = id;
  }
  loadSNMPJournal(snmpCursor);
  try {
    const devices = await api('/api/network');
    if (currentRoute.page !== 'snmp-events') return;
    const select = $('snmp-event-device');
    const selection = snmpFilters.device || '';
    const choices = [['', 'Все устройства'], ...devices.filter(d => d.method === 'snmp').map(d => [d.id, d.name])];
    if (selection && !choices.some(([key]) => key === selection)) choices.push([selection, 'Устройство ' + selection]);
    updateOptions(select, choices, selection);
  } catch (error) {
    if (currentRoute.page === 'snmp-events') msg('snmp-events-msg', error.message, true);
  }
}

async function loadSNMPJournal(cursor = 0, quiet = false) {
  const request = ++snmpRequest;
  const query = new URLSearchParams(snmpFilters);
  if (cursor) query.set('before', cursor);
  try {
    const data = await api('/api/network/events?' + query);
    if (request !== snmpRequest || currentRoute.page !== 'snmp-events') return;
    snmpCursor = cursor;
    snmpNext = data.next || 0;
    const rows = $('snmp-event-rows');
    const kinds = {availability: 'Доступность', interface: 'Интерфейс', trap: 'Trap / Inform'};
    rows.replaceChildren(data.items.length ? dataTable(['Получено', 'Устройство', 'Событие', 'Описание'], data.items.map(event => {
      const device = document.createElement('div');
      device.append(makeText('strong', event.device));
      if (event.source && event.source !== event.device) device.append(makeText('small', event.source, 'storage-cell-note'));
      const description = document.createElement('div');
      description.append(makeText('strong', event.message));
      if (event.oid) description.append(makeText('small', event.oid, 'storage-cell-note'));
      if (event.variables?.length) {
        const details = document.createElement('details');
        details.append(makeText('summary', 'Параметры SNMP'));
        for (const variable of event.variables) details.append(makeText('div', variable.oid + ' = ' + variable.value, 'syslog-message'));
        description.append(details);
      }
      return [new Date(event.at).toLocaleString('ru-RU'), device, kinds[event.kind] || event.kind, description];
    })) : emptyNote('Событий пока нет.'));
    $('snmp-older').classList.toggle('hidden', !snmpNext);
    $('snmp-latest').classList.toggle('hidden', !snmpCursor);
    msg('snmp-events-msg');
  } catch (error) {
    if (request === snmpRequest && !quiet && currentRoute.page === 'snmp-events') msg('snmp-events-msg', error.message, true);
  }
}

function refreshSNMPJournal() {
  if (!snmpCursor && !$('snmp-event-rows').querySelector('details[open]')) loadSNMPJournal(0, true);
}

$('snmp-refresh').addEventListener('click', () => loadSNMPJournal(snmpCursor));
$('snmp-latest').addEventListener('click', () => loadSNMPJournal());
$('snmp-older').addEventListener('click', () => { if (snmpNext) loadSNMPJournal(snmpNext); });
$('snmp-event-filter').addEventListener('submit', event => {
  event.preventDefault();
  snmpFilters = {device: $('snmp-event-device').value, kind: $('snmp-event-kind').value, q: $('snmp-event-query').value.trim()};
  loadSNMPJournal();
});

async function loadSNMPTrapSettings() {
  try {
    const state = await api('/api/settings/snmp');
    $('snmp-traps-enabled').checked = state.config.enabled;
    $('snmp-traps-bind').value = state.config.bind;
    $('snmp-traps-port').value = state.config.port;
    $('snmp-traps-community').value = '';
    $('snmp-traps-community').placeholder = state.hasCommunity ? 'Community сохранена' : '';
    msg('snmp-traps-msg', state.error || (state.active ? 'Приём активен, UDP ' + state.config.port : 'Приём Trap / Inform выключен.'), !!state.error);
  } catch (error) {
    msg('snmp-traps-msg', error.message, true);
  }
}

$('snmp-traps-toggle').addEventListener('click', async () => {
  const opened = $('snmp-traps-form').classList.toggle('hidden') === false;
  $('snmp-traps-toggle').setAttribute('aria-expanded', String(opened));
  if (opened) await loadSNMPTrapSettings();
  else {
    $('snmp-traps-community').value = '';
    msg('snmp-traps-msg');
  }
});
$('snmp-traps-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button = event.currentTarget.querySelector('button[type=submit]');
  button.disabled = true;
  try {
    await api('/api/settings/snmp', {method: 'PUT', body: JSON.stringify({
      enabled: $('snmp-traps-enabled').checked,
      bind: $('snmp-traps-bind').value.trim(),
      port: Number($('snmp-traps-port').value),
      community: $('snmp-traps-community').value
    })});
    await loadSNMPTrapSettings();
  } catch (error) {
    msg('snmp-traps-msg', error.message, true);
  } finally {
    button.disabled = false;
  }
});
