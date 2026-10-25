import type { Table } from "./chart.ts";

/** The accessible alternative to a chart: a captioned table of the same points, built with text content only. */
export function tableElement(doc: Document, t: Table): HTMLElement {
  const figure = doc.createElement("figure");
  const table = doc.createElement("table");
  const caption = doc.createElement("caption");
  caption.textContent = t.caption;
  table.append(caption);
  const head = doc.createElement("tr");
  for (const c of t.columns) {
    const th = doc.createElement("th");
    th.scope = "col";
    th.textContent = c;
    head.append(th);
  }
  const thead = doc.createElement("thead");
  thead.append(head);
  const body = doc.createElement("tbody");
  for (const r of t.rows) {
    const tr = doc.createElement("tr");
    tr.dataset.id = r.id;
    r.cells.forEach((text, i) => {
      const td = doc.createElement(i === 0 ? "th" : "td");
      if (i === 0) (td as HTMLTableCellElement).scope = "row";
      td.textContent = i === 0 && r.hidden ? `${text} (hidden in the chart)` : text;
      tr.append(td);
    });
    body.append(tr);
  }
  table.append(thead, body);
  const note = doc.createElement("figcaption");
  const omitted = t.omitted === 0 ? "" : ` ${t.omitted} points are not listed because of the active filters.`;
  note.textContent = `${t.note}${omitted}`;
  figure.append(table, note);
  return figure;
}
