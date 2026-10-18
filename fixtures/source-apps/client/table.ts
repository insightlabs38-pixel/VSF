import { SplitMix64 } from "@vsf/schema";

const ROW_COUNT = 10000;
const ROW_HEIGHT = 32;
const OVERSCAN = 3;
const CUSTOMERS = ["Acme", "Globex", "Initech", "Umbrella", "Hooli", "Stark", "Wayne", "Wonka"];

export function rowData(index: number): { id: number; customer: string; total: number } {
  const rng = new SplitMix64(index + 1);
  return { id: index + 1, customer: CUSTOMERS[rng.below(CUSTOMERS.length)] ?? "", total: rng.below(100000) };
}

const viewport = document.getElementById("viewport") as HTMLElement;
const spacer = document.getElementById("spacer") as HTMLElement;
const summary = document.getElementById("summary") as HTMLElement;
spacer.style.height = `${ROW_COUNT * ROW_HEIGHT}px`;

function render(): void {
  const first = Math.max(0, Math.floor(viewport.scrollTop / ROW_HEIGHT) - OVERSCAN);
  const last = Math.min(ROW_COUNT - 1, Math.ceil((viewport.scrollTop + viewport.clientHeight) / ROW_HEIGHT) + OVERSCAN);
  const fragment = document.createDocumentFragment();
  for (let i = first; i <= last; i++) {
    const d = rowData(i);
    const row = document.createElement("div");
    row.className = "row";
    row.setAttribute("role", "row");
    row.setAttribute("aria-rowindex", String(i + 2));
    row.style.top = `${i * ROW_HEIGHT}px`;
    for (const text of [String(d.id), d.customer, String(d.total)]) {
      const cell = document.createElement("span");
      cell.setAttribute("role", "cell");
      cell.textContent = text;
      row.append(cell);
    }
    fragment.append(row);
  }
  spacer.replaceChildren(fragment);
  summary.textContent = `Showing rows ${first + 1}-${last + 1} of ${ROW_COUNT}`;
}

viewport.addEventListener("scroll", () => requestAnimationFrame(render), { passive: true });
render();
