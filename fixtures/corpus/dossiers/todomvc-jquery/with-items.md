# TodoMVC (jquery-handlebars) - with-items
Upstream: https://github.com/tastejs/todomvc
Framework: jquery-handlebars
Ground truth basis: TodoMVC application specification (behaviour of add, toggle, edit, delete, filter, clear); Chromium accessibility tree
Page title: TodoMVC: jQuery
State: three todos added
What people do here (from the manifest):
- complete, edit, delete and filter todos
- clear completed todos

## Accessibility tree (Chromium, full page)
```yaml
- heading "todos" [level=1]
- textbox "What needs to be done?"
- main:
  - checkbox "❯ Mark all as complete"
  - text: ❯ Mark all as complete
  - list:
    - listitem:
      - checkbox
      - text: Buy milk
    - listitem:
      - checkbox
      - text: Walk the dog
    - listitem:
      - checkbox
      - text: Read a book
- strong: "3"
- text: items left
- list:
  - listitem:
    - link "All":
      - /url: "#/all"
  - listitem:
    - link "Active":
      - /url: "#/active"
  - listitem:
    - link "Completed":
      - /url: "#/completed"
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
