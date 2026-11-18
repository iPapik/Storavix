'use strict';

const geoMaps = new Map();
let locationRevision = 0;

function geoMap(id) {
  let view = geoMaps.get(id);
  if (!view) {
    const frame = document.createElement('div');
    frame.className = 'geo-frame';
    const canvas = document.createElement('div');
    canvas.className = 'geo-canvas';
    canvas.setAttribute('aria-label', 'Географическая карта OpenStreetMap');
    const status = makeText('div', '', 'geo-map-status hidden');
    status.setAttribute('role', 'status');
    if (typeof L === 'undefined') {
      frame.append(emptyNote('Не удалось загрузить библиотеку карты.'));
      return frame;
    }
    const map = L.map(canvas, {scrollWheelZoom: false, maxBounds: [[-90, -540], [90, 540]]});
    map.attributionControl.setPrefix(false);
    view = {frame, canvas, map, markers: L.layerGroup().addTo(map), positions: null, signature: null};
    if (canManageDevices()) frame.append(buildLocationControls(view));
    frame.append(canvas, status);
    const tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a>',
      referrerPolicy: 'strict-origin-when-cross-origin'
    }).addTo(map);
    tiles.on('tileerror', () => {
      status.textContent = 'Слой OpenStreetMap недоступен';
      status.classList.remove('hidden');
    });
    tiles.on('tileload', () => status.classList.add('hidden'));
    const observer = new ResizeObserver(() => {
      if (canvas.clientWidth && canvas.clientHeight) map.invalidateSize({pan: false});
    });
    observer.observe(canvas);
    view.dispose = () => { observer.disconnect(); map.remove(); };
    geoMaps.set(id, view);
  }

  view.syncDevices?.();
  const targets = mapTargets().map(target => ({...target, point: monitoring.locations[target.kind + ':' + target.id]}))
    .filter(target => target.point);
  const groups = new Map();
  for (const target of targets) {
    const key = target.point.lat.toFixed(6) + ':' + target.point.lon.toFixed(6);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(target);
  }
  const signature = JSON.stringify(targets.map(t => [t.kind, t.id, t.name, t.status, t.point]));
  if (view.signature !== signature) {
    view.signature = signature;
    view.markers.clearLayers();
    for (const list of groups.values()) {
      const {lat, lon} = list[0].point;
      const color = list.some(t => t.status === 'down') ? '#df737e' : list.some(t => t.status === 'warning') ? '#edb278' :
        list.every(t => t.status === 'up') ? '#45c88a' : '#8da6b3';
      const popup = document.createElement('div');
      popup.className = 'geo-popup';
      for (const target of list) {
        const item = document.createElement('div');
        item.append(routeLink(target.name, target.kind === 'server' ? 'server' : 'network', target.id),
          statusTag(target.status), makeText('small', target.point.address || lat.toFixed(5) + ', ' + lon.toFixed(5)));
        if (view.editPoint) item.append(action('Редактировать метку', () => view.editPoint(target.kind + ':' + target.id)));
        popup.append(item);
      }
      L.circleMarker([lat, lon], {radius: list.length > 1 ? 11 : 8, fillColor: color, color: '#162225', weight: 2,
        fillOpacity: .94, bubblingMouseEvents: false}).bindPopup(popup).addTo(view.markers);
    }
  }

  const positions = JSON.stringify([...groups.keys()].sort());
  if (positions !== view.positions) {
    view.positions = positions;
    const points = [...groups.values()].map(([target]) => [target.point.lat, target.point.lon]);
    requestAnimationFrame(() => {
      if (!view.frame.isConnected || positions !== view.positions) return;
      view.map.invalidateSize({pan: false});
      if (view.keepZoom) {
        view.keepZoom = false;
        return;
      }
      if (!points.length) view.map.setView([55, 38], 3);
      else if (points.length === 1) view.map.setView(points[0], 14);
      else view.map.fitBounds(points, {padding: [28, 28], maxZoom: 15});
    });
  }
  return view.frame;
}

function buildLocationControls(view) {
  const controls = document.createElement('div');
  controls.className = 'widget-geo-tools';
  const bar = document.createElement('div');
  bar.className = 'widget-geo-bar';
  const choose = document.createElement('select');
  choose.setAttribute('aria-label', 'Устройство для метки');
  const note = makeText('p', '', 'form-msg');
  note.setAttribute('role', 'status');
  const tell = (text, error = false) => {
    note.textContent = text;
    note.classList.toggle('error', error);
  };
  let picking = false, busy = false;
  const pick = action('Поставить метку', () => {
    setPicking(!picking);
    tell(picking ? 'Щёлкни по карте, чтобы сохранить метку.' : '');
  }, 'secondary small');
  const setPicking = value => {
    picking = value;
    pick.setAttribute('aria-pressed', String(value));
    pick.textContent = value ? 'Отмена' : 'Поставить метку';
    view.canvas.classList.toggle('geo-picking', value);
  };
  const advanced = document.createElement('details');
  advanced.className = 'geo-advanced';
  advanced.append(makeText('summary', 'Адрес и координаты'));
  const form = document.createElement('form');
  form.className = 'widget-geo-form';
  const address = document.createElement('input');
  address.maxLength = 300;
  address.placeholder = 'Город, улица, дом';
  const coordinate = (title, max) => {
    const input = document.createElement('input');
    input.type = 'number';
    input.min = -max;
    input.max = max;
    input.step = 'any';
    input.required = true;
    input.setAttribute('aria-label', title);
    input.placeholder = title;
    return input;
  };
  const lat = coordinate('Широта', 90), lon = coordinate('Долгота', 180);
  const results = document.createElement('div');
  results.className = 'toolbar-actions';
  const write = async (point, remove = false) => {
    const key = choose.value;
    if (!key || busy) return;
    busy = true;
    try {
      await api('/api/locations/' + key.replace(':', '/'), {
        method: remove ? 'DELETE' : 'PUT', body: remove ? undefined : JSON.stringify(point)
      });
      locationRevision++;
      if (remove) delete monitoring.locations[key];
      else monitoring.locations[key] = point;
      setPicking(false);
      if (choose.value === key) readPoint();
      view.keepZoom = true;
      renderOverview();
      tell(remove ? 'Метка удалена.' : 'Метка сохранена.');
    } catch (error) {
      tell(error.message, true);
    } finally {
      busy = false;
    }
  };
  const lookup = action('Найти адрес', async () => {
    const key = choose.value;
    const query = address.value.trim();
    if (!key || query.length < 3) {
      tell('Введите адрес для поиска.', true);
      return;
    }
    lookup.disabled = true;
    try {
      const points = await api('/api/locations/search', {method: 'POST', body: JSON.stringify({address: query})});
      if (choose.value !== key) return;
      results.replaceChildren(...points.map(p => action(p.address, () => {
        address.value = p.address;
        lat.value = p.lat;
        lon.value = p.lon;
        view.map.setView([p.lat, p.lon], 16);
        results.replaceChildren();
        tell('Адрес выбран. Сохраните метку.');
      }, 'secondary small')));
      tell(points.length ? 'Выберите адрес из результатов.' : 'Адрес не найден.');
    } catch (error) {
      tell(error.message, true);
    } finally {
      lookup.disabled = false;
    }
  }, 'secondary small');
  const save = makeText('button', 'Сохранить метку', 'primary small');
  save.type = 'submit';
  const remove = action('Удалить метку', () => write(null, true));
  const readPoint = () => {
    const p = monitoring.locations[choose.value];
    address.value = p?.address || '';
    lat.value = p?.lat ?? '';
    lon.value = p?.lon ?? '';
    remove.disabled = !p;
    results.replaceChildren();
  };

  const sync = () => {
    const choices = mapTargets().map(t => [t.kind + ':' + t.id, t.name]);
    const current = choose.value;
    const signature = JSON.stringify(choices);
    if (signature === view.deviceSignature) return;
    view.deviceSignature = signature;
    updateOptions(choose, choices, current);
    pick.disabled = !choices.length;
    advanced.classList.toggle('hidden', !choices.length);
    if (choose.value !== current) readPoint();
  };
  choose.addEventListener('change', () => { readPoint(); tell(picking ? 'Щёлкни по карте, чтобы сохранить метку.' : ''); });
  form.addEventListener('submit', e => {
    e.preventDefault();
    write({lat: lat.valueAsNumber, lon: lon.valueAsNumber, address: address.value.trim()});
  });
  view.map.on('click', ({latlng}) => {
    if (!picking) return;
    const point = latlng.wrap();
    write({lat: Number(point.lat.toFixed(7)), lon: Number(point.lng.toFixed(7))});
  });
  view.editPoint = key => { choose.value = key; readPoint(); advanced.open = true; choose.focus(); };
  view.syncDevices = sync;
  bar.append(choose, pick, advanced);
  const explanation = makeText('small', 'Поиск отправляет адрес геокодеру.', 'muted');
  form.append(address, lat, lon, lookup, save, remove, explanation, results);
  advanced.append(form);
  controls.append(bar, note);
  return controls;
}

function clearRemovedGeoWidgets(widgets) {
  const retained = new Set(widgets.filter(w => w.type === 'geomap').map(w => 'widget-' + w.id));
  for (const [id, view] of geoMaps) {
    if (!retained.has(id)) {
      view.dispose();
      geoMaps.delete(id);
    }
  }
}
