# TodoMVC (react) - with-items
Upstream: https://github.com/tastejs/todomvc
Framework: react
Ground truth basis: TodoMVC application specification (behaviour of add, toggle, edit, delete, filter, clear); Chromium accessibility tree
Page title: TodoMVC: React
State: three todos added
What people do here (from the manifest):
- complete, edit, delete and filter todos
- clear completed todos

## Accessibility tree (Chromium, full page)
```yaml
- heading "todos" [level=1]
- textbox "New Todo Input":
  - /placeholder: What needs to be done?
- main:
  - checkbox "❯ Toggle All Input"
  - text: ❯ Toggle All Input
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
- text: 3 items left!
- list:
  - listitem:
    - link "All":
      - /url: "#/"
  - listitem:
    - link "Active":
      - /url: "#/active"
  - listitem:
    - link "Completed":
      - /url: "#/completed"
- contentinfo:
  - paragraph: Double-click to edit a todo
  - paragraph: Created by the TodoMVC Team
  - paragraph:
    - text: Part of
    - link "TodoMVC":
      - /url: http://todomvc.com
```
