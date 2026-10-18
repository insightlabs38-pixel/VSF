const term = document.getElementById("term") as HTMLElement;
let line = "";
const scrollback: string[] = ["VSF terminal placeholder (no shell attached)"];

function paint(): void {
  term.textContent = `${scrollback.join("\n")}\n$ ${line}`;
}

term.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    scrollback.push(`$ ${line}`, `echo: ${line}`);
    line = "";
  } else if (event.key === "Backspace") {
    line = line.slice(0, -1);
  } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey) {
    line += event.key;
  } else {
    return;
  }
  event.preventDefault();
  paint();
});
paint();
