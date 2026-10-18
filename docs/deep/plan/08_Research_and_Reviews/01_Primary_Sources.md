# Primary source and research verification index

**Research date:** October 18, 2026. Links are supporting background/standards references, not claims of approval, certification or exhaustive legal review. Internet resources evolve; pin document versions and recheck standards immediately before implementation or release.

## Browser standards, extensions and runtime policy

- Chrome Web Store Manifest V3 remote code requirements: https://developer.chrome.com/docs/webstore/program-policies/mv3-requirements — packaged code, limits on remotely downloaded interpreters.
- Chrome remotely hosted code migration: https://developer.chrome.com/docs/extensions/develop/migrate/remote-hosted-code — downloaded scripts/WASM versus data.
- Chrome offscreen documents: https://developer.chrome.com/docs/extensions/reference/api/offscreen — service workers do not have DOM access; offscreen lifecycle constraints.
- Chrome service worker lifecycle: https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle — worker termination/restart behavior.
- Chrome Native Messaging: https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging — browser/companion communication and host registration.
- Chrome debugger API: https://developer.chrome.com/docs/extensions/reference/api/debugger — privileged inspection with user consent.
- WebDriver BiDi W3C Working Draft (Oct 8 2026): https://www.w3.org/TR/webdriver-bidi/ — cross-browser protocol; check implementation matrix.
- Chrome DevTools DOMSnapshot: https://chromedevtools.github.io/devtools-protocol/tot/DOMSnapshot/ — flattened DOM/style/layout snapshots.
- Chrome DevTools Accessibility: https://chromedevtools.github.io/devtools-protocol/tot/Accessibility/ — accessibility evidence and partial tree queries.
- Playwright auth state: https://playwright.dev/docs/auth — cookies/local storage/IndexedDB state and sensitivity warning.

## Identity, security and data handling

- RFC 10017 (Aug 2026) browser OAuth BCP: https://www.rfc-editor.org/rfc/rfc10017.html — supported browser OAuth patterns and PKCE.
- OAuth 2.0 Security Best Current Practice RFC 9700: https://www.rfc-editor.org/rfc/rfc9700.html
- OWASP SSRF Prevention: https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html
- OWASP Secrets Management: https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html
- OWASP MCP Top 10: https://github.com/OWASP/www-project-mcp-top-10
- W3C WCAG 2.2: https://www.w3.org/TR/WCAG22/
- HIPAA cloud service BAA guidance: https://www.hhs.gov/hipaa/for-professionals/special-topics/cloud-computing/index.html
- FERPA school-official guidance: https://studentprivacy.ed.gov/faq/under-what-conditions-can-third-party-service-provider-be-considered-school-official-under-ferpa

## Programmatic interfaces and native API examples

- MCP specification 2026-07-28 release: https://blog.modelcontextprotocol.io/posts/2026-07-28/
- MCP tool specification: https://modelcontextprotocol.io/specification/2026-07-28/server/tools
- Benchling V3 API overview: https://docs.benchling.com/docs/v3-api-overview — unified typed API, stable vs beta tier.
- Google Cloud Shell: https://cloud.google.com/shell/docs/using-cloud-shell
- Google Cloud APIs overview: https://cloud.google.com/apis/docs/overview

## Business and infrastructure references

- Whatfix company product and pricing: https://whatfix.com/pricing/
- Browserbase pricing: https://www.browserbase.com/pricing
- Cloudflare Browser Rendering pricing: https://developers.cloudflare.com/browser-rendering/platform/pricing/
- Retool product: https://retool.com/
- WalkMe acquisition by SAP (historical corporate signal): https://news.sap.com/2024/09/sap-completes-acquisition-walkme/

## Research interpretation limits

- Market comparables validate *adjacent customer demand*, not VSF's total market or future valuation.
- Hosted browser prices exclude many costs; measure actual utilization and support.
- Provider API availability is tenant, feature and license scoped.
- Published browser standards are not equivalent to every browser release supporting every command.
- Trademark and vendor-term analysis requires separate formal legal review; no clearance performed here.

## Post-review verification updates

See `04_Research_Between_Reviews.md` for targeted official-source rechecks performed between artifact reviews and resulting edits.
