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

function orderedWidgets(widgets) {
  return [...widgets].sort((a, b) => (a.layout?.y ?? widgetRows) - (b.layout?.y ?? widgetRows) ||
    (a.layout?.x ?? 0) - (b.layout?.x ?? 0));
}

function packWidgets(widgets, compact = false) {
  const layout = compact ? new Map() : new Map(widgets.filter(widget => widget.layout).map(widget => [widget.id, widget.layout]));
  for (const widget of widgets) {
    if (layout.has(widget.id)) continue;
    const map = widget.type === 'geomap';
    const room = [...layout.values()];
    const w = widget.layout?.w ?? (widget.wide ? 12 : 6);
    const h = widget.layout?.h ?? (map ? 6 : 4);
    place: for (let y = 0; y < widgetRows; y++) {
      for (let x = 0; x <= widgetColumns - w; x++) {
        const position = {x, y, w, h};
        if (widgetFits(position, room)) {
          layout.set(widget.id, position);
          break place;
        }
      }
    }
  }
  return layout;
}

function layoutWidgets(widgets, compact = false) {
  return packWidgets(compact ? orderedWidgets(widgets) : widgets, compact);
}

function applyWidgetLayout(widgets, positions) {
  for (const widget of widgets) widget.layout = positions.get(widget.id);
}

function settleWidgetDraft() {
  // Follow the *new* coordinates, not the original widget array order.
  // Otherwise a dragged card jumps back on mouse release.
  const ordered=orderedWidgets(dashboardDraft.widgets);
  applyWidgetLayout(ordered,packWidgets(ordered,true));
  dashboardDraft.widgets=ordered;
}

function refreshWidgetHandle(widget, sizing) {
  renderOverview();
  const panel = [...$('widget-grid').children].find(el => el.dataset.widget === widget.id);
  panel?.querySelector(sizing ? '.widget-grip' : '.widget-move')?.focus({preventScroll: true});
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

  const apply = (dx, dy, initial) => {
    const p = {...initial};
    if (sizing) {
      p.w = Math.max(3, Math.min(widgetColumns - p.x, p.w + dx));
      p.h = Math.max(2, Math.min(12, p.h + dy));
    } else {
      p.x = Math.max(0, Math.min(widgetColumns - p.w, p.x + dx));
      p.y = Math.max(0, Math.min(widgetRows - 1, p.y + dy));
    }
    widget.layout = p;
    drawWidgetPosition(panel, p);
  };
  handle.addEventListener('keydown', event => {
    if (!Object.hasOwn(widgetArrows, event.key)) return;
    event.preventDefault();
    const [dx, dy] = widgetArrows[event.key];
    if (sizing) {
      apply(dx, dy, widget.layout);
      settleWidgetDraft();
    } else {
      const sorted = orderedWidgets(dashboardDraft.widgets);
      const index = sorted.indexOf(widget);
      const target = index + dx + dy;
      if (target < 0 || target >= sorted.length) return;
      [sorted[index], sorted[target]] = [sorted[target], sorted[index]];
      applyWidgetLayout(sorted, packWidgets(sorted, true));
      dashboardDraft.widgets=sorted;
    }
    refreshWidgetHandle(widget, sizing);
    msg('dashboard-msg', 'Столбец ' + (widget.layout.x + 1) + ', строка ' + (widget.layout.y + 1)
      + ' · ' + widget.layout.w + ' × ' + widget.layout.h);
  });
  // Moving a widget works from the whole header. Resize remains on the
  // bottom-right grip. Use a GPU transform while dragging and commit the grid
  // coordinates once, on release, to avoid jittery reflows.
  const surface = sizing ? handle : panel;
  const begin = event => {
    if (!event.isPrimary || event.button !== 0 || !matchMedia('(min-width: 851px)').matches) return;
    if (!sizing) {
      if (!event.target.closest('.dashboard-widget > header')) return;
      if (event.target.closest('a, input, select, .widget-remove')) return;
    }
    event.preventDefault();
    const initial={...widget.layout};
    const grid=$('widget-grid');
    const gap=Number.parseFloat(getComputedStyle(grid).columnGap)||0;
    const rowStep=(Number.parseFloat(getComputedStyle(grid).gridAutoRows)||62)+gap;
    const colStep=(grid.clientWidth+gap)/widgetColumns;
    const startX=event.clientX, startY=event.clientY;
    const startScroll=window.scrollY;
    let lastX=startX,lastY=startY,frame=0;
    const position = () => ({
      dx:lastX-startX,
      dy:lastY-startY+window.scrollY-startScroll
    });
    surface.setPointerCapture(event.pointerId);
    panel.classList.add('widget-dragging');
    msg('dashboard-msg');
    const preview=()=>{
      frame=0;
      const {dx,dy}=position();
      if(sizing) {
        apply(Math.round(dx/colStep),Math.round(dy/rowStep),initial);
      } else {
        panel.style.transform='translate3d('+dx+'px,'+dy+'px,0)';
      }
    };
    const move=e=>{
      if(e.pointerId!==event.pointerId)return;
      lastX=e.clientX;lastY=e.clientY;
      if(!frame)frame=requestAnimationFrame(preview);
    };
    const done=()=>{
      if(frame)cancelAnimationFrame(frame);
      const {dx,dy}=position();
      panel.style.transform='';
      apply(Math.round(dx/colStep),Math.round(dy/rowStep),initial);
      panel.classList.remove('widget-dragging');
      surface.removeEventListener('pointermove',move);
      surface.removeEventListener('lostpointercapture',done);
      if(dashboardDraft){settleWidgetDraft();refreshWidgetHandle(widget,sizing)}
    };
    surface.addEventListener('pointermove',move);
    surface.addEventListener('lostpointercapture',done,{once:true});
  };
  surface.addEventListener('pointerdown',begin);

  return handle;
}
