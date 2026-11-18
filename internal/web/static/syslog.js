'use strict';

const severityNames = ['Авария', 'Тревога', 'Критично', 'Ошибка', 'Предупреждение', 'Уведомление', 'Информация', 'Отладка'];
const facilities = ['kernel', 'user', 'mail', 'daemon', 'auth', 'syslog', 'lpr', 'news', 'uucp', 'clock', 'authpriv', 'ftp',
  'ntp', 'audit', 'alert', 'clock2', 'local0', 'local1', 'local2', 'local3', 'local4', 'local5', 'local6', 'local7'];

for (const [value, title] of severityNames.entries()) $('syslog-severity').append(new Option(title, value));
$('syslog-severity').append(new Option('Без уровня', '-1'));

function syslogStateLabel(state) {
  if (state.error) return state.error;
  if (!state.config.enabled) return 'Приём выключен';
  if (!state.active.length) return 'Ожидание запуска';
  return state.active.map(p => p.toUpperCase()).join(' / ') + ' · ' + state.config.bind + ' · порт ' + state.config.port +
    (state.drops ? ' · пропущено с момента запуска: ' + state.drops : '');
}

function putSyslogFields(state) {
  const c = state.config;
  $('syslog-enable').checked = c.enabled;
  $('syslog-bind').value = c.bind;
  $('syslog-port').value = c.port;
  $('syslog-limit').value = c.limit;
  $('syslog-udp').checked = c.udp;
  $('syslog-tcp').checked = c.tcp;
  $('syslog-state').textContent = syslogStateLabel(state);
}

async function loadSyslogSettings() {
  try {
    putSyslogFields(await api('/api/settings/syslog'));
  } catch (error) {
    msg('syslog-settings-msg', error.message, true);
  }
}

$('syslog-settings-form').addEventListener('submit', async event => {
  event.preventDefault();
  const button = event.currentTarget.querySelector('button[type=submit]');
  button.disabled = true;
  try {
    const config = {
      enabled: $('syslog-enable').checked, bind: $('syslog-bind').value.trim(),
      port: Number($('syslog-port').value), limit: Number($('syslog-limit').value),
      udp: $('syslog-udp').checked, tcp: $('syslog-tcp').checked
    };
    const state = await api('/api/settings/syslog', {method: 'PUT', body: JSON.stringify(config)});
    putSyslogFields(state);
    msg('syslog-settings-msg', 'Настройки сохранены.');
  } catch (error) {
    msg('syslog-settings-msg', error.message, true);
  } finally {
    button.disabled = false;
  }
});

let syslogCursor = 0, syslogNext = 0, syslogRequest = 0;
let syslogFilters = {};

function openSyslogJournal(reset) {
  if (reset) syslogCursor = 0;
  loadSyslogJournal(syslogCursor);
}

async function loadSyslogJournal(cursor = 0, quiet = false) {
  const request = ++syslogRequest;
  try {
    const query = new URLSearchParams(syslogFilters);
    if (cursor) query.set('before', cursor);
    const [data, state] = await Promise.all([
      api('/api/syslog?' + query), isAdmin() ? api('/api/settings/syslog') : null
    ]);
    if (request !== syslogRequest) return;
    syslogCursor = cursor;
    syslogNext = data.next || 0;
    const status = $('syslog-listener-status');
    status.textContent = state ? syslogStateLabel(state) : '';
    status.className = state ? 'tag ' + (state.error ? 'storage-warning' : state.active.length ? 'green' : 'grey') : 'hidden';
    renderSyslogRows(data.items);
    $('syslog-older').classList.toggle('hidden', !syslogNext);
    $('syslog-latest').classList.toggle('hidden', !syslogCursor);
    msg('syslog-msg');
  } catch (error) {
    if (request === syslogRequest && !quiet) msg('syslog-msg', error.message, true);
  }
}

function renderSyslogRows(items) {
  const container = $('syslog-rows');
  if (!items.length) {
    container.replaceChildren(emptyNote('Подходящих сообщений пока нет.'));
    return;
  }
  const rows = items.map(entry => {
    const received = makeText('time', new Date(entry.receivedAt).toLocaleString('ru-RU'));
    received.dateTime = entry.receivedAt;
    if (entry.timestamp) received.title = 'Время отправителя: ' + entry.timestamp;
    const source = document.createElement('div');
    source.append(makeText('strong', entry.host || entry.source));
    const metadata = [entry.host && entry.host !== entry.source ? entry.source : '', entry.transport.toUpperCase()]
      .filter(Boolean).join(' · ');
    source.append(makeText('small', metadata, 'storage-cell-note'));
    const level = document.createElement('div');
    level.append(makeText('span', severityNames[entry.severity] || 'Без уровня',
      'tag ' + (entry.severity < 0 ? 'grey' : entry.severity <= 3 ? 'storage-failed' : entry.severity === 4 ? 'storage-warning' : 'grey')));
    if (facilities[entry.facility]) level.append(makeText('small', facilities[entry.facility], 'storage-cell-note'));
    const message = document.createElement('div');
    if (entry.app) message.append(makeText('strong', entry.app));
    message.append(makeText('div', entry.message, 'syslog-message'));
    return [received, source, level, message];
  });
  container.replaceChildren(dataTable(['Получено', 'Источник', 'Уровень', 'Сообщение'], rows));
}

$('syslog-filter').addEventListener('submit', event => {
  event.preventDefault();
  syslogFilters = {source: $('syslog-source').value.trim(), severity: $('syslog-severity').value, q: $('syslog-query').value.trim()};
  loadSyslogJournal();
});
$('syslog-refresh').addEventListener('click', () => loadSyslogJournal(syslogCursor));
$('syslog-latest').addEventListener('click', () => loadSyslogJournal());
$('syslog-older').addEventListener('click', () => {
  if (syslogNext) loadSyslogJournal(syslogNext);
});
