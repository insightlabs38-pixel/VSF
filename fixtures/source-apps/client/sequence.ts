const input = document.getElementById("seq") as HTMLTextAreaElement;
const stats = document.getElementById("stats") as HTMLElement;

export function analyze(text: string): { length: number; gc: number; invalid: string[] } {
  const clean = text.replace(/\s+/g, "").toUpperCase();
  const invalid = [...new Set(clean.replace(/[ACGT]/g, ""))].sort();
  const gcCount = [...clean].filter((c) => c === "G" || c === "C").length;
  return {
    length: clean.length,
    gc: clean.length === 0 ? 0 : Math.round((gcCount * 1000) / clean.length) / 10,
    invalid,
  };
}

function update(): void {
  const r = analyze(input.value);
  if (r.invalid.length > 0) {
    input.setAttribute("aria-invalid", "true");
    stats.className = "error";
    stats.textContent = `Invalid characters: ${r.invalid.join(" ")}`;
  } else {
    input.removeAttribute("aria-invalid");
    stats.className = "";
    stats.textContent = `Length ${r.length}, GC ${r.gc}%`;
  }
}

input.addEventListener("input", update);
update();
