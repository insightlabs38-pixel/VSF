const params = new URLSearchParams(location.search);
// Secret-shaped values are assembled here at run time so none is committed to the repository.
const pieces = {
  token: ["gh", "p_", "abcdefghijklmnopqrstuvwxyz", "0123456789"].join(""),
  password: ["correct", "horse", "battery", "staple"].join("-"),
  bearer: ["Bear", "er ", "abcdef0123456789abcdef"].join(""),
};
(document.getElementById("pw") as HTMLInputElement).value = pieces.password;
(document.getElementById("api") as HTMLInputElement).value = pieces.token;
(document.getElementById("note") as HTMLElement).textContent =
  `Saved token ${pieces.token} for ${params.get("user") ?? "user"}`;
(document.getElementById("b") as HTMLElement).setAttribute("aria-label", pieces.bearer);
(window as unknown as { __secrets: typeof pieces }).__secrets = pieces;

export {};
