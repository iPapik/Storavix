'use strict';

const widgetColumns = 12;
const widgetRows = 192;
const widgetArrows = {ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1]};

function widgetsOverlap(a, b) {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

function widgetFits(layout, others) {
  return layout.x >= 0 && layout.y >= 0 && layout.w >= 3 && layout.h >= 2 && layout.h <= 12 &&
    layout.y < widgetRows && layout.x + layout.w <= widgetColumns && !others.some(other => widgetsOverlap(layout, other));
}

function layoutWidgets(widgets) {
  const layout = new Map(widgets.filter(widget => widget.layout).map(widget => [widget.id, widget.layout]));
  for (const widget of widgets) {
    if (layout.has(widget.id)) continue;
    const map = widget.type === 'map' || widget.type === 'geomap';
    const room = [...layout.values()];
    const placed = {x: 0, y: 0, w: widget.wide ? 12 : 6, h: map ? 6 : 4};
    for (let y = 0; y < widgetRows; y++) {
      placed.y = y;
      for (let x = 0; x <= widgetColumns - placed.w; x++) {
        placed.x = x;
        if (widgetFits(placed, room)) break;
      }
      if (widgetFits(placed, room)) {
        layout.set(widget.id, placed);
        break;
      }
    }
  }
  return layout;
}

function drawWidgetPosition(panel, pos) {
  panel.style.gridColumn = (pos.x + 1) + ' / span ' + pos.w;
  panel.style.gridRow = (pos.y + 1) + ' / span ' + pos.h;
}

function widgetHandle(widget, panel, sizing) {
  const label = sizing ? 'Изменить размер виджета' : 'Переместить виджет';
  const handle = makeText('button', sizing ? '◢' : '⠿', sizing ? 'widget-grip' : 'widget-move');
  handle.type = 'button';
  handle.title = label + ' · используйте мышь или стрелки';
  handle.setAttribute('aria-label', label);

  const apply = (dx, dy, initial, announce = false) => {
    const p = {...initial};
    if (sizing) {
      p.w = Math.max(3, Math.min(widgetColumns - p.x, p.w + dx));
      p.h = Math.max(2, Math.min(12, p.h + dy));
    } else {
      p.x = Math.max(0, Math.min(widgetColumns - p.w, p.x + dx));
      p.y = Math.max(0, Math.min(widgetRows - 1, p.y + dy));
    }
    const others = dashboardDraft.widgets.filter(item => item !== widget).map(item => item.layout);
    const fits = widgetFits(p, others);
    panel.classList.toggle('widget-blocked', !fits && !announce);
    if (fits) {
      widget.layout = p;
      drawWidgetPosition(panel, p);
    }
    if (announce) {
      msg('dashboard-msg', fits ? 'Столбец ' + (p.x + 1) + ', строка ' + (p.y + 1) + ' · ' + p.w + ' × ' + p.h
        : 'Эта область уже занята другим виджетом.', !fits);
    }
  };
  handle.addEventListener('keydown', event => {
    if (!Object.hasOwn(widgetArrows, event.key)) return;
    event.preventDefault();
    apply(...widgetArrows[event.key], widget.layout, true);
  });
  handle.addEventListener('pointerdown', event => {
    if (!event.isPrimary || event.button !== 0 || !matchMedia('(min-width: 851px)').matches) return;
    event.preventDefault();
    const initial = {...widget.layout};
    const grid = $('widget-grid');
    const gap = Number.parseFloat(getComputedStyle(grid).columnGap);
    const rowStep = Number.parseFloat(getComputedStyle(grid).gridAutoRows) + gap;
    const colStep = (grid.clientWidth + gap) / widgetColumns;
    const start = {x: event.clientX, y: event.clientY, scroll: window.scrollY};
    handle.setPointerCapture(event.pointerId);
    panel.classList.add('widget-dragging');
    msg('dashboard-msg');
    const move = e => {
      if (e.pointerId !== event.pointerId) return;
      apply(Math.round((e.clientX - start.x) / colStep),
        Math.round((e.clientY - start.y + window.scrollY - start.scroll) / rowStep), initial);
      const direction = e.clientY > window.innerHeight - 52 ? 1 : e.clientY < 52 ? -1 : 0;
      if (direction) window.scrollBy(0, direction * 40);
    };
    const done = () => {
      panel.classList.remove('widget-dragging', 'widget-blocked');
      handle.removeEventListener('pointermove', move);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('lostpointercapture', done, {once: true});
  });
  return handle;
}
