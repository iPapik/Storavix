'use strict';
const $=id=>document.getElementById(id);

const ICONS={
layout:'<rect x="3" y="3" width="7" height="8" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="3" y="15" width="7" height="6" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/>',
servers:'<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 8h.01M7 17h.01"/>',
drives:'<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 14h18M7 17h.01M11 17h.01"/>',
network:'<rect x="9" y="2" width="6" height="5" rx="1"/><rect x="2" y="17" width="6" height="5" rx="1"/><rect x="16" y="17" width="6" height="5" rx="1"/><path d="M12 7v6M5 17v-4h14v4"/>',
logs:'<path d="M8 6h13M8 12h13M8 18h13M3 6h1M3 12h1M3 18h1"/>',
bell:'<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/>',
settings:'<circle cx="12" cy="12" r="4"/><path d="M12 2v4M12 18v4M2 12h4M18 12h4M5 5l3 3M16 16l3 3M19 5l-3 3M8 16l-3 3"/>',
cpu:'<rect x="6" y="6" width="12" height="12" rx="1.5"/><path d="M9 9h6v6H9zM9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
memory:'<rect x="2" y="7" width="20" height="10" rx="2"/><path d="M6 7V5M10 7V5M14 7V5M18 7V5M6 17v2M10 17v2M14 17v2M18 17v2M6 11h1M10 11h1M14 11h1"/>',
drive:'<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 14h18M7 17h.01M11 17h.01"/>',
gpu:'<rect x="3" y="5" width="18" height="14" rx="2"/><rect x="7" y="8" width="9" height="8" rx="1"/>',
activity:'<path d="M2 12h5l3-7 4 14 3-7h5"/>',
thermometer:'<path d="M10 15V5a2 2 0 0 1 4 0v10a4 4 0 1 1-4 0ZM12 10v9"/>',
wifi:'<path d="M2 9a16 16 0 0 1 20 0M5 13a11 11 0 0 1 14 0M8.5 16a6 6 0 0 1 7 0"/>',
services:'<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M4 10h16M10 4v16"/>',
package:'<path d="m12 2 9 5v10l-9 5-9-5V7l9-5ZM3 7l9 5 9-5M12 12v10"/>',
clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l4 2"/>',
radio:'<circle cx="12" cy="12" r="2"/><path d="M7 7a7 7 0 0 0 0 10M17 7a7 7 0 0 1 0 10M4 4a11 11 0 0 0 0 16M20 4a11 11 0 0 1 0 16"/>',
refresh:'<path d="M20 7v5h-5M4 17v-5h5M5 10a8 8 0 0 1 14-3l1 5M19 14a8 8 0 0 1-14 3l-1-5"/>',
menu:'<path d="M4 6h16M4 12h16M4 18h16"/>',
trash:'<path d="M3 6h18M8 6V4h8v2M6 6l1 15h10l1-15M10 10v7M14 10v7"/>',
info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7h.01"/>'
};
function makeIcon(name){const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('class','ui-icon');svg.setAttribute('aria-hidden','true');svg.innerHTML=ICONS[name]||ICONS.info;return svg}
document.querySelectorAll('.sidebar .nav-icon').forEach((el,i)=>el.replaceWith(makeIcon(['layout','servers','drives','network','logs','bell','settings'][i]||'info')));
document.querySelectorAll('.stat-icon').forEach((el,i)=>el.replaceChildren(makeIcon(['servers','drives','drive','bell'][i]||'info')));
document.querySelectorAll('.empty-icon').forEach((el,i)=>el.replaceChildren(makeIcon(i===0?'drives':'logs')));
$('menu-toggle').replaceChildren(makeIcon('menu'));
$('systems-refresh').replaceChildren(makeIcon('refresh'),document.createTextNode(' Обновить'));

let session=false;
const pages={overview:['Обзор инфраструктуры','Единый центр состояния серверов, хранилищ и сетевых устройств','OVERVIEW'],servers:['Все системы','Нажмите на систему для просмотра дополнительной информации.','SERVERS'],storage:['RAID и диски','Контроллеры, массивы и физические накопители','STORAGE'],network:['Сетевые устройства','SNMP, интерфейсы, порты и сетевой трафик','NETWORK'],syslog:['Централизованный Syslog','Журналы и события всех устройств в одном месте','SYSLOG'],alerts:['События и алерты','История событий и доставка уведомлений','ALERTS'],settings:['Настройки','Почтовый сервер, безопасность и управление конфигурацией','SETTINGS']};
const generic={servers:['memory','Серверы появятся после подключения агентов','Установите агенты Storavix на Windows и Linux. Они будут отправлять показатели CPU, RAM, сети и доступности.',['Windows / Linux','Показатели CPU и RAM','Установленные агенты','Обнаружение ОС']],network:['network','Мониторинг сетевой инфраструктуры','Добавим устройства по SNMPv3, карты интерфейсов, показатели трафика и уведомления об ошибках портов.',['SNMPv3','Коммутаторы','Маршрутизаторы','Трафик интерфейсов']],syslog:['logs','Централизованный сбор Syslog','Приём журналов с серверов, коммутаторов и маршрутизаторов: UDP, TCP, TLS с поиском и фильтрами.',['Syslog UDP / TCP','Syslog TLS','Поиск событий','Правила алертов']]};
async function api(url,opts={}){let r;try{r=await fetch(url,{credentials:'same-origin',headers:{'Content-Type':'application/json','X-Storavix-Request':'1',...(opts.headers||{})},...opts});}catch(e){throw new Error('Нет соединения с сервером Storavix')}let j;try{j=await r.json()}catch(_){throw new Error('Неверный ответ сервера')}if(!r.ok){if(r.status===401){showAuth();throw new Error('Сессия истекла. Войдите заново.')}throw new Error(j.error||`HTTP ${r.status}`)}return j}
function msg(id,text,error=false){const el=$(id);el.textContent=text;el.classList.toggle('error',error)}
function showAuth(){$('shell').classList.add('hidden');$('login').classList.remove('hidden');session=false}
function showShell(){$('login').classList.add('hidden');$('shell').classList.remove('hidden');session=true;goto('overview')}
function goto(p){if(!pages[p])return;document.querySelectorAll('[data-page]').forEach(b=>b.classList.toggle('active',b.dataset.page===p));['overview','generic','alerts','settings','servers','storage'].forEach(n=>$(n+'-view').classList.add('hidden'));if(['network','syslog'].includes(p)){$('generic-view').classList.remove('hidden');const c=generic[p];$('generic-symbol').replaceChildren(makeIcon(c[0]));$('generic-title').textContent=c[1];$('generic-description').textContent=c[2];$('generic-features').replaceChildren(...c[3].map(x=>{const v=document.createElement('span');v.textContent=x;return v}))}else{$(p+'-view').classList.remove('hidden')}
document.querySelector('.heading').classList.toggle('hidden',p==='servers');$('page-title').textContent=pages[p][0];$('page-description').textContent=pages[p][1];$('breadcrumb').textContent=pages[p][0];$('eyebrow-page').textContent=pages[p][2];document.querySelector('.sidebar').classList.remove('open');if(p==='overview')loadOverview();if(p==='servers'||p==='storage')loadSystems();if(p==='alerts')loadAlerts();if(p==='settings')loadMail()}
async function loadOverview(){try{const a=await api('/api/summary');['hosts','arrays','drives','events'].forEach(n=>$('metric-'+n).textContent=a[n]);$('version').textContent='v'+a.version.replace('-dev','');renderStorageSummary(a);document.querySelector('.staging').textContent=a.hosts?'● Подключено агентов: '+a.hosts:'● Ожидание агентов';document.querySelector('.onboarding h3').textContent=a.hosts?'Подключены агенты Storavix':'Storavix готов к работе';document.querySelector('.onboarding p').textContent=a.hosts?'Сбор метрик CPU, RAM, диска и сети активен. Подробные сведения доступны в разделе «Все системы».':'Веб-интерфейс запущен. Установите агент Storavix на Windows или Linux и введите регистрационный токен.';$('mail-status').textContent=a.mailConfigured?'Настроено':'Не настроено';$('mail-status').className='tag '+(a.mailConfigured?'green':'grey');$('mail-dot').className='component-bullet '+(a.mailConfigured?'success':'idle');const list=await api('/api/alerts');recent(list)}catch(e){console.warn(e.message)}}
function recent(events){if(!events.length)return;const box=$('recent-alerts');box.className='';box.replaceChildren(...events.slice(0,4).map(ev=>eventNode(ev)))}
function eventNode(ev){const row=document.createElement('div');row.className='alert-row';const date=document.createElement('span');date.className='alert-date';date.textContent=new Date(ev.createdAt).toLocaleString('ru-RU');const info=document.createElement('div');const title=document.createElement('strong');title.textContent=ev.subject;const sub=document.createElement('small');sub.textContent=ev.detail;info.append(title,sub);const status=document.createElement('span');status.className=ev.delivered?'sent':'failed';status.textContent=ev.delivered?'Отправлено':'Ошибка отправки';row.append(date,info,status);return row}
async function loadAlerts(){try{const ev=await api('/api/alerts');const list=$('alerts-list');list.replaceChildren();if(!ev.length){list.innerHTML='<div class="empty-compact"><span class="empty-icon"></span><strong>История событий пуста</strong><p>Система пока не получала события. Тестовую отправку можно проверить в настройках.</p></div>';list.querySelector('.empty-icon').replaceChildren(makeIcon('bell'));return}list.append(...ev.map(eventNode))}catch(e){console.warn(e.message)}}
async function loadMail(){try{const c=await api('/api/settings/mail');$('mail-host').value=c.host||'';$('mail-port').value=c.port||587;$('mail-security').value=c.security||'starttls';$('mail-auth').value=c.auth||'plain';$('mail-username').value=c.username||'';$('mail-from').value=c.from||'';$('mail-recipients').value=c.recipients||'';$('mail-ca').value=c.caFile||'';$('mail-password').value='';$('mail-password').placeholder=c.hasSecret?'Секрет сохранён — оставьте пустым':'Введите пароль или access token';mailMode()}catch(e){msg('smtp-msg',e.message,true)}}
function mailMode(){const a=$('mail-auth').value;const none=a==='none';$('mail-username').disabled=none;$('mail-password').disabled=none;if($('mail-security').value==='none'&&!none){$('mail-auth').value='none';$('mail-username').disabled=true;$('mail-password').disabled=true}if($('mail-security').value==='none'&&$('mail-port').value==='587')$('mail-port').value='25'}
$('login-form').addEventListener('submit',async e=>{e.preventDefault();msg('login-msg','Выполняется вход...');try{await api('/api/login',{method:'POST',body:JSON.stringify({username:$('login-user').value,password:$('login-pass').value})});$('login-pass').value='';msg('login-msg','');showShell()}catch(err){msg('login-msg',err.message,true)}});
$('logout').addEventListener('click',async()=>{try{await api('/api/logout',{method:'POST'})}finally{showAuth()}});
document.querySelectorAll('[data-page]').forEach(b=>b.addEventListener('click',()=>goto(b.dataset.page)));
document.querySelectorAll('[data-goto]').forEach(b=>b.addEventListener('click',()=>goto(b.dataset.goto)));
$('open-settings').addEventListener('click',()=>goto('settings'));
$('menu-toggle').addEventListener('click',()=>document.querySelector('.sidebar').classList.toggle('open'));
$('alert-refresh').addEventListener('click',loadAlerts);
document.querySelectorAll('[data-tab]').forEach(b=>b.addEventListener('click',()=>{document.querySelectorAll('[data-tab]').forEach(x=>x.classList.toggle('active',x===b));$('smtp-settings').classList.toggle('hidden',b.dataset.tab!=='smtp');$('account-settings').classList.toggle('hidden',b.dataset.tab!=='account')}));
$('mail-auth').addEventListener('change',mailMode);$('mail-security').addEventListener('change',()=>{if($('mail-security').value==='tls'&&$('mail-port').value==='587')$('mail-port').value=465;if($('mail-security').value==='starttls'&&$('mail-port').value==='465')$('mail-port').value=587;mailMode()});
$('smtp-form').addEventListener('submit',async e=>{e.preventDefault();const c={host:$('mail-host').value.trim(),port:Number($('mail-port').value),security:$('mail-security').value,auth:$('mail-auth').value,username:$('mail-username').value.trim(),password:$('mail-password').value,from:$('mail-from').value.trim(),recipients:$('mail-recipients').value.trim(),caFile:$('mail-ca').value.trim(),hasSecret:false};try{await api('/api/settings/mail',{method:'PUT',body:JSON.stringify(c)});msg('smtp-msg','Настройки сохранены. Можно отправить тестовое письмо.');$('mail-password').value='';await loadMail()}catch(e){msg('smtp-msg',e.message,true)}});
async function sendTest(endpoint){msg('smtp-msg','Отправляем письмо...');try{await api(endpoint,{method:'POST'});msg('smtp-msg','Письмо успешно отправлено. Проверьте входящие и папку «Спам».')}catch(e){msg('smtp-msg',e.message,true)}}
$('test-email').addEventListener('click',()=>sendTest('/api/settings/mail/test'));
$('test-alert').addEventListener('click',()=>sendTest('/api/alerts/test'));
$('password-form').addEventListener('submit',async e=>{e.preventDefault();try{await api('/api/account/password',{method:'POST',body:JSON.stringify({old:$('old-password').value,new:$('new-password').value})});$('old-password').value='';$('new-password').value='';msg('password-msg','Пароль изменён, войдите заново.');setTimeout(showAuth,900)}catch(err){msg('password-msg',err.message,true)}});
(async function boot(){try{const s=await api('/api/session');if(s.authenticated)showShell();else showAuth()}catch(e){showAuth();msg('login-msg',e.message,true)}})();

const panelAddress=document.getElementById('panel-address');if(panelAddress)panelAddress.textContent=window.location.host;


// Agent-backed system cards: no placeholders and no health claims without measurements.
let systemsCache=[];
const systemsLayoutModes=['large','standard','compact','list'];
let systemsLayout='large';
try { const stored=localStorage.getItem('storavix.systems.layout'); if (systemsLayoutModes.includes(stored)) systemsLayout=stored; } catch (_) {}
$('systems-layout').value=systemsLayout;
const fmtPercent=x=>typeof x==='number'?x.toLocaleString('ru-RU',{maximumFractionDigits:1})+'%':'—';
const fmtRate=x=>typeof x!=='number'?'—':x>=1048576?(x/1048576).toLocaleString('ru-RU',{maximumFractionDigits:2})+' MB/s':x>=1024?(x/1024).toLocaleString('ru-RU',{maximumFractionDigits:1})+' KB/s':x.toLocaleString('ru-RU',{maximumFractionDigits:0})+' B/s';
const fmtUptime=x=>typeof x!=='number'?'—':x>=86400?Math.floor(x/86400)+' дн.':x>=3600?Math.floor(x/3600)+' ч.':Math.floor(x/60)+' мин.';
function metricRow(icon,title,val,pct,warning=75){const row=document.createElement('div');row.className='system-metric';const ic=document.createElement('span');ic.className='system-icon';ic.replaceChildren(makeIcon(icon));const titleNode=document.createElement('span');titleNode.className='system-label';titleNode.textContent=title;const value=document.createElement('strong');value.className='system-value';value.textContent=val;row.append(ic,titleNode,value);if(pct!==undefined){const bar=document.createElement('div');bar.className='system-bar';const fill=document.createElement('span');fill.className=pct>=warning?'system-fill warning':'system-fill';fill.style.width=(typeof pct==='number'?Math.max(0,Math.min(pct,100)):0)+'%';bar.append(fill);row.append(bar)}return row}
function stateFor(r){return Date.now()-Date.parse(r.receivedAt)>120000?'offline':storageHasWarning(r)?'warn':'online'}
function makeSystemCard(r){const status=stateFor(r);const card=document.createElement('article');card.className='system-card';card.tabIndex=0;card.setAttribute('role','button');card.setAttribute('aria-label','Подробнее о сервере '+r.hostname);const head=document.createElement('div');head.className='system-head';const name=document.createElement('b');name.textContent=r.hostname;const dot=document.createElement('i');dot.className='system-dot '+status;head.append(dot,name);const state=document.createElement('span');state.className='system-state';state.textContent=status==='online'?'● Онлайн':status==='warn'?'RAID: внимание':'● Нет связи';head.append(state);card.append(head);
 const m=document.createElement('div');m.className='system-metrics';m.append(metricRow('cpu','ЦП:',fmtPercent(r.cpuPercent),r.cpuPercent),metricRow('memory','Память:',fmtPercent(r.memoryPercent),r.memoryPercent),metricRow('drive','Диск:',fmtPercent(r.diskPercent),r.diskPercent,90),metricRow('network','Сеть:',fmtRate(r.networkBytesPerSec)),metricRow('clock','Время работы:',fmtUptime(r.uptimeSeconds)),metricRow('drive','RAID:',fmtArrayCount(r)),metricRow('radio','Агент:',r.version||'—'));
 card.append(m);const expand=()=>showSystemDetail(r);card.addEventListener('click',expand);card.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();expand()}});return card}
function renderSystems(){const grid=$('systems-grid');const f=$('systems-filter').value.trim().toLowerCase();const systems=systemsCache.filter(x=>x.hostname.toLowerCase().includes(f)||x.os.toLowerCase().includes(f));grid.classList.remove('large-layout','standard-layout','compact-layout','list-layout');grid.classList.add(systemsLayout+'-layout');grid.replaceChildren(...systems.map(makeSystemCard));$('systems-empty').classList.toggle('hidden',systemsCache.length!==0);$('systems-note').classList.toggle('hidden',systemsCache.length===0);if(systemsCache.length&&!systems.length){const none=document.createElement('p');none.className='systems-note';none.textContent='По фильтру ничего не найдено.';grid.append(none)}}
async function loadSystems() {
  try {
    const agents = await api('/api/agents');
    systemsCache = agents;
    renderSystems();
    renderStorage();
    const modal = $('system-detail');
    if (!modal.classList.contains('hidden')) {
      const report = agents.find(agent => agent.id === modal.dataset.agent);
      if (report && !$('detail-content').querySelector('button:disabled')) showSystemDetail(report);
    }
  } catch (e) {
    console.warn(e.message);
    $('storage-servers').replaceChildren(makeText('p', e.message, 'systems-note'));
  }
}
function showSystemDetail(r) {
  $('system-detail').dataset.agent = r.id;
  $('detail-name').textContent = r.hostname;
  const d = $('detail-content');
  const open = new Set([...d.querySelectorAll('details[open]')].map(node => node.dataset.storageKey));
  d.replaceChildren();
  const fields = [
    ['Система', r.os], ['Последний опрос', new Date(r.receivedAt).toLocaleString('ru-RU')],
    ['ЦП', fmtPercent(r.cpuPercent)], ['Память', fmtPercent(r.memoryPercent)],
    ['Оперативная память', r.memoryBytes ? fmtBytes(r.memoryBytes) : null],
    ['Доступно памяти', r.memoryAvailable != null ? fmtBytes(r.memoryAvailable) : null],
    ['Заполнение системного раздела', fmtPercent(r.diskPercent)],
    ['Сеть (приём + передача)', fmtRate(r.networkBytesPerSec)],
    ['Средняя загрузка (1, 5, 15 мин.)', r.loadAverage?.length === 3 ? r.loadAverage.join(' / ') : null],
    ['Время работы', fmtUptime(r.uptimeSeconds)], ['Версия агента', r.version]
  ];
  for (const [key, value] of fields) detailRow(d, key, value ?? '—');
  if (r.volumes?.length) {
    d.append(makeText('h3', 'Файловые системы'));
    d.append(dataTable(
      ['Том', 'Тип', 'Объём', 'Свободно', 'Занято'],
      r.volumes.map(v => [v.name, v.filesystem || '—', fmtBytes(v.sizeBytes), fmtBytes(v.availableBytes),
        fmtPercent(100 * (1 - v.availableBytes / v.sizeBytes))])
    ));
  }
  d.append(makeText('h3', 'RAID и физические диски'));
  d.append(storageInventory(r, open));
  const actions=document.createElement('div');actions.className='detail-actions';
const tip=document.createElement('p');tip.className='muted';tip.textContent='Удалённое устройство не сможет вернуться с прежним ID агента.';
const del=document.createElement('button');del.type='button';del.className='danger-button';del.append(makeIcon('trash'),document.createTextNode(' Удалить устройство'));
del.addEventListener('click',async()=>{if(!confirm('Удалить «'+r.hostname+'» из мониторинга? Для повторного подключения потребуется новый ID агента.'))return;del.disabled=true;try{await api('/api/agents/'+encodeURIComponent(r.id),{method:'DELETE'});$('system-detail').classList.add('hidden');await loadSystems()}catch(err){alert('Удаление не выполнено: '+err.message);del.disabled=false}});
actions.append(tip,del);d.append(actions);
$('system-detail').classList.remove('hidden')}
$('systems-filter').addEventListener('input',renderSystems);
$('systems-layout').addEventListener('change',()=>{const selected=$('systems-layout').value;if(!systemsLayoutModes.includes(selected))return;systemsLayout=selected;try{localStorage.setItem('storavix.systems.layout',selected)}catch(_){}renderSystems()});
$('systems-refresh').addEventListener('click',loadSystems);
$('system-detail-close').addEventListener('click',()=>$('system-detail').classList.add('hidden'));
$('system-detail').addEventListener('click',e=>{if(e.target===$('system-detail'))$('system-detail').classList.add('hidden')});
document.addEventListener('keydown',e=>{if(e.key==='Escape')$('system-detail').classList.add('hidden')});
setInterval(()=>{if(session&&['servers','storage'].some(p=>!$(p+'-view').classList.contains('hidden')))loadSystems()},30000);

function fmtArrayCount(report) {
  if (report.arrays?.length) return String(report.arrays.length);
  return report.storage?.sources?.some(source => source.status === 'ok') ? '0' : '—';
}

const storageStates = {
  active: ['В норме', 'green'], degraded: ['Деградация', 'storage-warning'],
  rebuilding: ['Восстановление', 'storage-warning'], inactive: ['Недоступно', 'storage-failed'],
  unknown: ['Неизвестно', 'grey']
};
const storageSources = {
  'linux-md': 'Linux MD', 'windows-spaces': 'Storage Spaces',
  'linux-raid-hardware': 'Аппаратный RAID Linux', 'storcli-perccli': 'StorCLI / PercCLI',
  storcli: 'StorCLI', storcli2: 'StorCLI2', perccli: 'PercCLI', perccli2: 'PercCLI2',
  ssacli: 'HPE SSA CLI', 'inventory-limit': 'Лимит инвентаризации'
};
const storageSourceStates = {
  ok: 'опрошен', partial: 'частичные данные', error: 'ошибка опроса', unavailable: 'недоступен'
};
const driveRoles = {
  member: 'Участник', spare: 'Резервный', 'global-spare': 'Общий резервный',
  jbod: 'JBOD', unassigned: 'Свободный', pool: 'Диск пула', journal: 'Журнал', retired: 'Выведен'
};

function makeText(tag, value, className) {
  const node = document.createElement(tag);
  node.textContent = value;
  if (className) node.className = className;
  return node;
}

function fmtBytes(amount) {
  if (typeof amount !== 'number') return '—';
  const units = ['Б', 'КиБ', 'МиБ', 'ГиБ', 'ТиБ', 'ПиБ', 'ЭиБ'];
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit++;
  }
  return amount.toLocaleString('ru-RU', {maximumFractionDigits: 2}) + ' ' + units[unit];
}

function healthTag(status) {
  const [label, color] = storageStates[status] || storageStates.unknown;
  return makeText('span', label, 'tag ' + color);
}

function storageHasWarning(report) {
  const states = [...(report.arrays || []), ...(report.storage?.drives || []), ...(report.storage?.controllers || [])];
  return states.some(item => ['degraded', 'inactive', 'rebuilding'].includes(item.status))
    || report.storage?.sources?.some(source => source.status === 'error' || source.status === 'partial');
}

function detailRow(parent, label, value) {
  const row = document.createElement('div');
  row.className = 'detail-row';
  row.append(makeText('span', label), makeText('strong', value));
  parent.append(row);
}

function dataTable(columns, rows) {
  const wrap = document.createElement('div');
  wrap.className = 'storage-table-wrap';
  const table = document.createElement('table');
  table.className = 'storage-table';
  const headers = table.createTHead().insertRow();
  for (const column of columns) {
    const header = makeText('th', column);
    header.scope = 'col';
    headers.append(header);
  }

  const body = table.createTBody();
  for (const values of rows) {
    const row = body.insertRow();
    for (const value of values) {
      const cell = row.insertCell();
      if (value instanceof Node) cell.append(value);
      else cell.textContent = value ?? '—';
    }
  }
  wrap.append(table);
  return wrap;
}

function driveTable(drives) {
  return dataTable(
    ['Слот / устройство', 'Модель', 'Серийный номер', 'Тип', 'Объём', 'Роль', 'Состояние'],
    drives.map(d => {
      const health = document.createElement('div');
      health.append(healthTag(d.status));
      if (d.detail) health.append(makeText('small', d.detail, 'storage-cell-note'));
      if (d.progress != null) health.append(makeText('small', fmtPercent(d.progress), 'storage-cell-note'));
      if (d.temperature != null) health.append(makeText('small', d.temperature + ' °C', 'storage-cell-note'));
      const model = document.createElement('div');
      model.append(makeText('span', d.model || '—'));
      if (d.firmware) model.append(makeText('small', 'FW ' + d.firmware, 'storage-cell-note'));
      return [[d.slot, d.device].filter(Boolean).join(' · ') || '—', model, d.serial || '—',
        [d.interface, d.media].filter(Boolean).join(' / ') || '—', d.sizeBytes ? fmtBytes(d.sizeBytes) : '—',
        driveRoles[d.role] || d.role || '—', health];
    })
  );
}

function arrayInfo(array, drives, open) {
  const card = document.createElement('details');
  card.className = 'storage-array';
  card.dataset.storageKey = JSON.stringify([array.controller, array.group, array.name]);
  card.open = open.has(card.dataset.storageKey);
  const summary = document.createElement('summary');
  const name = [array.name, array.level, array.group].filter(Boolean).join(' · ');
  summary.append(makeText('strong', name), healthTag(array.status));
  card.append(summary);
  const fields = [
    ['Объём', array.sizeBytes ? fmtBytes(array.sizeBytes) : null],
    ['Ожидается дисков', array.expected || null],
    ['Отсутствует дисков', array.missing || null],
    ['Прогресс', array.progress != null ? fmtPercent(array.progress) : null],
    ['Состояние по данным источника', array.detail]
  ];
  for (const [key, value] of fields) if (value != null && value !== '') detailRow(card, key, value);
  const members = (array.members || []).map(id => drives.get(id)).filter(Boolean);
  if (members.length) card.append(driveTable(members));
  else card.append(makeText('p', 'Источник не передал состав массива.', 'muted'));
  return card;
}

function storageInventory(report, open = new Set()) {
  const host = document.createElement('div');
  host.className = 'storage-inventory';
  const inventory = report.storage;
  const arrays = report.arrays || [];
  if (Date.now() - Date.parse(report.receivedAt) > 120000) {
    host.append(makeText('p', 'Сервер вне сети. Показаны последние полученные данные.', 'muted'));
  }
  if (inventory) {
    const sources = document.createElement('p');
    sources.className = 'storage-sources';
    const collected = new Date(inventory.collectedAt);
    sources.append(makeText('strong', 'Опрос хранилища ' + collected.toLocaleString('ru-RU')));
    for (const source of inventory.sources || []) {
      const name = storageSources[source.name] || source.name;
      sources.append(makeText('span', name + ' · ' + storageSourceStates[source.status],
        'tag ' + (source.status === 'error' || source.status === 'partial' ? 'storage-warning' : 'grey')));
    }
    host.append(sources);
  }
  if (fmtArrayCount(report) !== '—' || (inventory?.drives || []).length) {
    host.append(makeText('p', 'Обнаружено массивов: ' + fmtArrayCount(report) +
      (inventory ? ' · Физических дисков: ' + (inventory.drives || []).length : ''), 'muted'));
  }
  const drives = new Map((inventory?.drives || []).map(drive => [drive.id, drive]));
  const controllers = inventory?.controllers || [];
  for (const c of controllers) {
    const block = document.createElement('div');
    block.className = 'storage-controller';
    const header = document.createElement('div');
    header.className = 'storage-controller-head';
    header.append(makeText('h4', c.model || c.id), healthTag(c.status));
    block.append(header);
    const identity = [storageSources[c.source] || c.source, c.serial && ('S/N ' + c.serial),
      c.firmware && ('FW ' + c.firmware), c.detail].filter(Boolean);
    if (identity.length) block.append(makeText('p', identity.join(' · '), 'muted'));
    const list = arrays.filter(a => a.controller === c.id);
    for (const a of list) block.append(arrayInfo(a, drives, open));
    const attached = (inventory.drives || []).filter(drive => drive.controller === c.id);
    if (attached.length) {
      const physical = document.createElement('details');
      physical.className = 'storage-array';
      physical.dataset.storageKey = c.id;
      physical.open = open.has(c.id);
      physical.append(makeText('summary', 'Все физические диски (' + attached.length + ')'), driveTable(attached));
      block.append(physical);
    }
    host.append(block);
  }
  for (const a of arrays.filter(a => !a.controller)) host.append(arrayInfo(a, drives, open));
  if (!controllers.length && !arrays.length) {
    const available = inventory?.sources?.some(source => source.status === 'ok');
    const message = !inventory ? 'Ожидание инвентаризации. Проверьте версию агента.' :
      available ? 'Опрошенные источники не обнаружили RAID-массивы.' : 'Нет доступной инвентаризации RAID.';

    host.append(makeText('p', message, 'muted'));
  }
  return host;
}

function renderStorage() {
  const root = $('storage-servers');
  const filter = $('storage-filter').value.toLowerCase().trim();
  const open = new Set([...root.querySelectorAll('details[open]')].map(node => node.dataset.agent || (node.closest('[data-agent]').dataset.agent + '/' + node.dataset.storageKey)));
  const reports = systemsCache.filter(report =>
    [report.hostname, ...(report.storage?.controllers || []).map(c => c.model)].some(value => value.toLowerCase().includes(filter)));
  if (!reports.length) {
    root.replaceChildren(makeText('p', systemsCache.length ? 'По фильтру ничего не найдено.' : 'Нет подключённых серверов.', 'systems-note'));
    return;
  }
  root.replaceChildren(...reports.map(report => {
    const section = document.createElement('details');
    section.className = 'panel storage-host';
    section.dataset.agent = report.id;
    section.open = open.has(report.id) || (reports.length === 1 && !root.children.length);
    const title = document.createElement('summary');
    title.append(makeText('strong', report.hostname), makeText('span', report.os, 'muted'));
    if (Date.now() - Date.parse(report.receivedAt) > 120000) title.append(makeText('span', 'Нет связи', 'tag grey'));
    else if (storageHasWarning(report)) title.append(makeText('span', 'Требуется внимание', 'tag storage-warning'));
    title.append(makeText('span', 'Массивы: ' + fmtArrayCount(report), 'tag grey'));
    const prefix = report.id + '/';
    const expanded = [...open].filter(key => key?.startsWith(prefix)).map(key => key.slice(prefix.length));
    section.append(title, storageInventory(report, new Set(expanded)));
    return section;
  }));
}

function renderStorageSummary(summary) {
  const root = $('overview-storage');
  root.replaceChildren(makeText('strong', !summary.hosts ? 'Ожидание данных от агентов' :
    summary.arrays || summary.drives ? 'Массивы: ' + summary.arrays + ' · Диски: ' + summary.drives :
    'Пока нет данных о массивах и дисках'));
}

$('storage-filter').addEventListener('input', renderStorage);
$('storage-refresh').addEventListener('click', loadSystems);
