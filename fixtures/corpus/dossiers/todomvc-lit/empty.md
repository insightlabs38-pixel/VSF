# TodoMVC (lit) - empty
Upstream: https://github.com/tastejs/todomvc
Framework: lit
Ground truth basis: TodoMVC application specification (behaviour of add, toggle, edit, delete, filter, clear); Chromium accessibility tree
Page title: TodoMVC: Lit
State: as loaded
What people do here (from the manifest):
- add a todo

## Accessibility tree (Chromium, full page)
```yaml
- heading "todos" [level=1]
- textbox "What needs to be done?"
- contentinfo:
  - paragraph: Double-click to edit a todo
  - paragraph:
    - text: Written by
    - link "the lit team":
      - /url: https://github.com/lit
  - paragraph:
    - text: Part of
    - link "TodoMVC":
      - /url: http://todomvc.com
```
