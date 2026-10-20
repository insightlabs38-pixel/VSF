# jQuery UI demos - accordion-default
Upstream: https://github.com/jquery/jquery-ui
Framework: jquery
Ground truth basis: jQuery UI demo descriptions and API documentation; Chromium accessibility tree
Page title: jQuery UI Accordion - Default functionality
State: as loaded
What people do here (from the manifest):
- open one section of an accordion

## Accessibility tree (Chromium, full page)
```yaml
- tablist:
  - tab "Section 1" [expanded] [selected]
  - tabpanel "Section 1":
    - paragraph: Mauris mauris ante, blandit et, ultrices a, suscipit eget, quam. Integer ut neque. Vivamus nisi metus, molestie vel, gravida in, condimentum sit amet, nunc. Nam a nibh. Donec suscipit eros. Nam mi. Proin viverra leo ut odio. Curabitur malesuada. Vestibulum a velit eu ante scelerisque vulputate.
  - tab "Section 2"
  - tab "Section 3"
  - tab "Section 4"
- paragraph: Click headers to expand/collapse content that is broken into logical sections, much like tabs. Optionally, toggle sections open/closed on mouseover.
- paragraph: The underlying HTML markup is a series of headers (H3 tags) and content divs so the content is usable without JavaScript.
```
