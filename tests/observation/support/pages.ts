export const SECRET_INNER = "CROSS-ORIGIN-INNER-SECRET";

export const scopesPage = (cross: string): string => `<!doctype html>
<html lang="en"><head><title>scopes</title></head><body>
<main id="root">
  <h1>Scopes</h1>
  <div id="open-host"></div>
  <my-closed id="closed-host"></my-closed>
  <my-plain id="plain-custom">light text</my-plain>
  <iframe id="same-doc" srcdoc="<p>inner same-origin</p><button>inside</button>"></iframe>
  <iframe id="same-src" src="/inner"></iframe>
  <iframe id="cross" src="${cross}/inner"></iframe>
  <iframe id="sandboxed" sandbox="allow-scripts" src="/inner"></iframe>
  <canvas id="c" width="10" height="10"></canvas>
  <object id="o" data="/x"></object>
</main>
<script>
  const open = document.getElementById("open-host").attachShadow({ mode: "open" });
  open.innerHTML = '<section><span id="deep-host"></span></section>';
  const nested = open.getElementById("deep-host").attachShadow({ mode: "open" });
  nested.innerHTML = '<button aria-label="deep button">deep</button>';
  const closed = document.getElementById("closed-host").attachShadow({ mode: "closed" });
  closed.innerHTML = '<button>closed content</button>';
  window.__vsfClosed = new WeakMap([[document.getElementById("closed-host"), closed]]);
</script>
</body></html>`;

export const innerPage = `<!doctype html><html lang="en"><body><p>${SECRET_INNER}</p><button>inner button</button></body></html>`;
