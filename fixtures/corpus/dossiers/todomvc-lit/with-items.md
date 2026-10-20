# TodoMVC (lit) - with-items
Upstream: https://github.com/tastejs/todomvc
Framework: lit
Ground truth basis: TodoMVC application specification (behaviour of add, toggle, edit, delete, filter, clear); Chromium accessibility tree
Page title: TodoMVC: Lit
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
    - text: Written by
    - link "the lit team":
      - /url: https://github.com/lit
  - paragraph:
    - text: Part of
    - link "TodoMVC":
      - /url: http://todomvc.com
```
