'use strict';

const geoMaps = new Map();
let geoDevice = '';
let geoSearchSequence = 0;
let geoSearchPending = false;
let geoWritePending = false;
let geoRouteTarget = '';

function selectGeoRoute(id) {
  geoRouteTarget = id;
}

function locatedTargets() {
  return mapTargets().filter(target => monitoring.locations[target.kind + ':' + target.id]).map(target => ({
    ...target, ...monitoring.locations[target.kind + ':' + target.id]
  }));
}

function locationSummary(target) {
  return target.address || target.lat.toFixed(5) + ', ' + target.lon.toFixed(5);
}

function geoMap(id, editable = false) {
  let view = geoMaps.get(id);
  if (!view) {
    const frame = document.createElement('div');
    frame.className = 'geo-frame';
    const canvas = document.createElement('div');
    canvas.className = 'geo-canvas';
    canvas.setAttribute('aria-label', 'Географическая карта OpenStreetMap');
    const status = makeText('div', '', 'geo-map-status hidden');
    status.setAttribute('role', 'status');
    frame.append(canvas, status);
    if (typeof L === 'undefined') {
      frame.replaceChildren(emptyNote('Не удалось загрузить библиотеку карты.'));
      return frame;
    }
    const map = L.map(canvas, {scrollWheelZoom: editable, maxBounds: [[-90, -540], [90, 540]]});
    map.attributionControl.setPrefix(false);
    const tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>',
      referrerPolicy: 'strict-origin-when-cross-origin'
    }).addTo(map);
    tiles.on('tileerror', () => {
      status.textContent = 'Слой OpenStreetMap недоступен. Проверьте подключение к интернету.';
      status.classList.remove('hidden');
    });
    tiles.on('tileload', () => status.classList.add('hidden'));
    if (editable) {
      map.on('click', ({latlng}) => {
        if (!$('geo-device').value || geoWritePending) return;
        const {lat, lng} = latlng.wrap();
        setGeoCoordinates({lat, lon: lng});
        $('geo-results').replaceChildren();
        msg('geo-msg', 'Координаты выбраны на карте. Сохраните точку.');
      });
    }
    view = {frame, map, markers: L.layerGroup().addTo(map), positions: null, signature: null};
    const observer = new ResizeObserver(() => {
      if (canvas.clientWidth && canvas.clientHeight) map.invalidateSize({pan: false});
    });
    observer.observe(canvas);
    view.dispose = () => { observer.disconnect(); map.remove(); };
    geoMaps.set(id, view);
  }
  const targets = locatedTargets();
  const groups = new Map();
  for (const target of targets) {
    const key = target.lat.toFixed(6) + ':' + target.lon.toFixed(6);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(target);
  }
  const signatures = JSON.stringify(targets.map(t => [t.kind, t.id, t.name, t.lat, t.lon, t.address, t.status]));
  if (view.signature !== signatures) {
    view.signature = signatures;
    view.markers.clearLayers();
    for (const list of groups.values()) {
      const first = list[0];
      const color = list.some(t => t.status === 'down') ? '#df737e' : list.some(t => t.status === 'warning') ? '#edb278' :
        list.every(t => t.status === 'up') ? '#45c88a' : '#8da6b3';
      const popup = document.createElement('div');
      popup.className = 'geo-popup';
      for (const target of list) {
        const item = document.createElement('div');
        const link = routeLink(target.name, target.kind === 'server' ? 'server' : 'network', target.id);
        item.append(link, statusTag(target.status), makeText('small', locationSummary(target)));
        if (editable) item.append(action('Редактировать точку', () => {
          $('geo-device').value = target.kind + ':' + target.id;
          editGeoLocation();
          $('geo-address').focus();
        }));
        popup.append(item);
      }
      L.circleMarker([first.lat, first.lon], {
        radius: list.length > 1 ? 11 : 8, fillColor: color, color: '#162225', weight: 2, fillOpacity: .94
      }).bindPopup(popup).addTo(view.markers);
    }
  }
  const positions = JSON.stringify([...groups.keys()].sort());
  if (positions !== view.positions) {
    view.positions = positions;
    const points = [...groups.values()].map(([target]) => [target.lat, target.lon]);
    // Size the view after the map element has been attached to its current panel.
    requestAnimationFrame(() => {
      if (!view.frame.isConnected || positions !== view.positions) return;
      view.map.invalidateSize({pan: false});
      if (!points.length) view.map.setView([55, 38], 3);
      else if (points.length === 1) view.map.setView(points[0], 14);
      else view.map.fitBounds(points, {padding: [28, 28], maxZoom: 15});
    });
  } else requestAnimationFrame(() => {
    if (view.frame.isConnected) view.map.invalidateSize({pan: false});
  });
  return view.frame;
}

function clearRemovedGeoWidgets(widgets) {
  const retained = new Set(widgets.filter(w => w.type === 'geomap').map(w => 'widget-' + w.id));
  for (const [id, view] of geoMaps) {
    if (id !== 'page' && !retained.has(id)) {
      view.dispose();
      geoMaps.delete(id);
    }
  }
}

function renderGeoPage() {
  const map = $('geo-page-map');
  const frame = geoMap('page', true);
  if (map.firstElementChild !== frame) map.replaceChildren(frame);
  const choices = mapTargets().map(t => [t.kind + ':' + t.id, t.name + (t.kind === 'server' ? ' · сервер' : ' · коммутатор')]);
  const select = $('geo-device');
  const preferred = select.value;
  const target = geoRouteTarget && choices.find(([key]) => key.split(':')[1] === geoRouteTarget);
  if (target) geoRouteTarget = '';
  updateOptions(select, choices, target?.[0] || geoDevice || preferred);
  if (select.value !== geoDevice) editGeoLocation();
  $('geo-search').disabled = !choices.length || geoSearchPending;
  $('geo-save').disabled = !choices.length || geoWritePending;
  $('geo-remove').disabled = !monitoring.locations[select.value] || geoWritePending;
  const list = $('geo-locations');
  const locations = locatedTargets();
  list.replaceChildren(makeText('h2', 'Точки на карте (' + locations.length + ')'));
  addList(list, locations, target => {
    const row = document.createElement('div');
    row.className = 'widget-list-row';
    const info = document.createElement('div');
    info.append(routeLink(target.name, target.kind === 'server' ? 'server' : 'network', target.id),
      makeText('small', locationSummary(target), 'muted'));
    row.append(info, action('На карте', () => {
      const view = geoMaps.get('page');
      view?.map.setView([target.lat, target.lon], 16);
      map.scrollIntoView({block: 'nearest'});
    }));
    return row;
  }, 'Выберите устройство и сохраните адрес или координаты.');
}

function setGeoCoordinates(point) {
  $('geo-lat').value = point ? Number(point.lat.toFixed(7)) : '';
  $('geo-lon').value = point ? Number(point.lon.toFixed(7)) : '';
}

function editGeoLocation() {
  geoDevice = $('geo-device').value;
  geoSearchSequence++;
  const point = monitoring.locations[geoDevice];
  $('geo-address').value = point?.address || '';
  setGeoCoordinates(point);
  $('geo-remove').disabled = !point || geoWritePending;
  $('geo-results').replaceChildren();
  msg('geo-msg');
}

$('geo-device').addEventListener('change', editGeoLocation);
$('geo-search').addEventListener('click', async () => {
  const address = $('geo-address').value.trim();
  if (address.length < 3) {
    msg('geo-msg', 'Введите адрес для поиска.', true);
    return;
  }
  const request = ++geoSearchSequence;
  const button = $('geo-search');
  geoSearchPending = true;
  button.disabled = true;
  msg('geo-msg', 'Ищем адрес…');
  try {
    const points = await api('/api/locations/search', {method: 'POST', body: JSON.stringify({address})});
    if (request !== geoSearchSequence) return;
    msg('geo-msg', points.length ? 'Выберите подходящий адрес.' : 'Адрес не найден. Уточните запрос или введите координаты.');
    const results = $('geo-results');
    results.replaceChildren(...points.map(point => action(point.address, () => {
      $('geo-address').value = point.address;
      setGeoCoordinates(point);
      geoMaps.get('page')?.map.setView([point.lat, point.lon], 16);
      results.replaceChildren();
      msg('geo-msg', 'Адрес выбран. Сохраните точку.');
    }, 'secondary small')));
  } catch (error) {
    if (request === geoSearchSequence) msg('geo-msg', error.message, true);
  } finally {
    geoSearchPending = false;
    button.disabled = !$('geo-device').value;
  }
});

async function writeGeoPoint(remove) {
  if (!geoDevice || geoWritePending) return;
  const key = geoDevice;
  const button = $(remove ? 'geo-remove' : 'geo-save');
  const point = {lat: $('geo-lat').valueAsNumber, lon: $('geo-lon').valueAsNumber, address: $('geo-address').value.trim()};
  geoWritePending = true;
  button.disabled = true;
  try {
    await api('/api/locations/' + key.replace(':', '/'), {
      method: remove ? 'DELETE' : 'PUT', body: remove ? undefined : JSON.stringify(point)
    });
    if (remove) delete monitoring.locations[key];
    else monitoring.locations[key] = point;
    if (geoDevice === key) editGeoLocation();
    renderGeoPage();
    msg('geo-msg', remove ? 'Точка удалена.' : 'Местоположение сохранено.');
  } catch (error) {
    msg('geo-msg', error.message, true);
  } finally {
    geoWritePending = false;
    $('geo-save').disabled = !$('geo-device').value;
    $('geo-remove').disabled = !monitoring.locations[$('geo-device').value];
  }
}
$('geo-form').addEventListener('submit', event => { event.preventDefault(); writeGeoPoint(false); });
$('geo-remove').addEventListener('click', () => writeGeoPoint(true));
