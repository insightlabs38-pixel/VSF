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

export const PRIVATE_VALUE = "TYPED-PRIVATE-VALUE-123";

export const labelsPage = `<!doctype html>
<html lang="ar" dir="rtl"><head><title>labels</title></head><body>
<form id="f">
  <label for="n">الاسم</label>
  <input id="n" type="text" value="${PRIVATE_VALUE}">
  <label id="wl">Wrapped <input id="w" type="text"></label>
  <fieldset disabled><legend>Group</legend>
    <input id="d" aria-label="disabled child"><button id="db" type="button">Go</button>
  </fieldset>
  <button id="lb" type="button" aria-labelledby="t1 t2">ignored content</button>
  <span id="t1">First</span> <span id="t2">Second</span>
  <input id="desc" aria-label="described" aria-describedby="help"><span id="help">Help text</span>
  <div dir="ltr"><span id="ltrspan">English in rtl document</span></div>
  <table><tr><th>H</th><td><button id="nb" type="button">Nested <b>bold</b> name</button></td></tr></table>
  <button id="hid" type="button" hidden>hidden button</button>
  <button id="marks" type="button">‏مرحبا‎ Hello</button>
  <input id="img" type="image" alt="Image button" src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">
  <select id="sel" aria-label="Pick"><option>One</option></select>
</form>
</body></html>`;

export const listPage = (rows: number): string => `<!doctype html>
<html lang="en"><body>
<main>
  <h1 id="title">Orders</h1>
  <ul id="list">${Array.from({ length: rows }, (_, i) => `<li id="r${i}"><span class="n">Row ${i}</span> <button type="button">Open ${i}</button></li>`).join("")}</ul>
  <section id="side"><p id="note">note</p></section>
</main>
</body></html>`;

export const SECRETS = {
  password: "hunter2-typed-password",
  apiToken: "tok_live_9f8e7d6c5b4a39281706",
  aws: "AKIAIOSFODNN7EXAMPLE",
  bearer: "Bearer abcdefghijklmnop1234567890",
  passphrase: "correct horse battery staple",
  name: "Alice Q. Applegate",
  email: "alice.applegate@example.com",
  card: "4111 1111 1111 1111",
  phone: "+1 (415) 555-0132",
  balance: "$1,204.55",
};

export const secretsPage = `<!doctype html>
<html lang="en"><body>
<main>
  <h1>Account settings</h1>
  <form>
    <label for="pw">Password</label><input id="pw" type="password">
    <label for="tk">API token</label><input id="tk" type="text" name="api_token">
    <label for="cc">Card</label><input id="cc" type="text" autocomplete="cc-number">
    <label for="free">Notes</label><input id="free" type="text">
  </form>
  <section data-vsf-scope="account"><p id="who">Signed in as ${SECRETS.name}</p><p id="bal">Balance ${SECRETS.balance}</p></section>
  <section data-vsf-scope="credential"><p id="pass">${SECRETS.passphrase}</p></section>
  <p id="mail">Contact ${SECRETS.email}</p>
  <p id="card-text">Card on file ${SECRETS.card}</p>
  <p id="tel">Call ${SECRETS.phone}</p>
  <pre id="log">key ${SECRETS.aws} header Authorization: ${SECRETS.bearer}</pre>
  <p id="plain">Plain public sentence about orders.</p>
  <div id="host"></div>
</main>
<script>
  const root = document.getElementById("host").attachShadow({ mode: "open" });
  root.innerHTML = '<p id="inshadow">${SECRETS.passphrase} inside shadow</p>';
  document.getElementById("host").setAttribute("data-vsf-scope", "credential");
</script>
</body></html>`;

export const spaPage = `<!doctype html>
<html lang="en"><body>
<nav><a id="go-orders" href="/spa/orders" data-route>Orders</a> <a id="go-customers" href="/spa/customers" data-route>Customers</a> <a id="go-home" href="/spa" data-route>Home</a></nav>
<main id="view"></main>
<script>
  const views = {
    "/spa": "<h1>Home</h1><p>Welcome</p>",
    "/spa/orders": "<h1>Orders</h1><ul><li>Order A</li><li>Order B</li></ul>",
    "/spa/customers": "<h1>Customers</h1><table><tr><th>Name</th></tr><tr><td>Zed</td></tr></table>",
  };
  const render = () => { document.getElementById("view").innerHTML = views[location.pathname] ?? "<h1>Not found</h1>"; };
  document.addEventListener("click", (e) => {
    const a = e.target.closest("a[data-route]");
    if (!a) return;
    e.preventDefault();
    history.pushState({}, "", a.getAttribute("href"));
    render();
  });
  addEventListener("popstate", render);
  render();
</script>
</body></html>`;

export const mpaPage = (n: number): string => `<!doctype html>
<html lang="en"><body><h1>Page ${n}</h1><p>content of page ${n}</p><a id="next" href="/mpa/${n + 1}">next</a></body></html>`;

export const framesPage = (cross: string): string => `<!doctype html>
<html lang="en"><body><h1>Frames</h1>
<iframe id="f1" src="/frames/a"></iframe>
<iframe id="f2" src="${cross}/frames/x"></iframe>
</body></html>`;

export const framePage = (label: string): string => `<!doctype html>
<html lang="en"><body><h2>frame ${label}</h2><button>btn ${label}</button></body></html>`;

export const richPage = `<!doctype html>
<html lang="en"><body>
<div id="box"><p id="p1">one</p><p id="p2">two</p><button id="b1" type="button">go</button></div>
<div id="shadow-host"></div>
<iframe id="inner" srcdoc="<ul id='il'><li>a</li></ul>"></iframe>
<p id="editable" contenteditable="true">edit me</p>
<script>
  const root = document.getElementById("shadow-host").attachShadow({ mode: "open" });
  root.innerHTML = '<span id="s1">shadow one</span><button id="sb" type="button" aria-label="shadow button">x</button>';
</script>
</body></html>`;
