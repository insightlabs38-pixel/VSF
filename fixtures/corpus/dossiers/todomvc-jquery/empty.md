# TodoMVC (jquery-handlebars) - empty
Upstream: https://github.com/tastejs/todomvc
Framework: jquery-handlebars
Ground truth basis: TodoMVC application specification (behaviour of add, toggle, edit, delete, filter, clear); Chromium accessibility tree
Page title: TodoMVC: jQuery
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
    - text: Created by
    - link "Sindre Sorhus":
      - /url: http://sindresorhus.com
  - paragraph: Updated by the TodoMVC Team
  - paragraph:
    - text: Part of
    - link "TodoMVC":
      - /url: http://todomvc.com
```
