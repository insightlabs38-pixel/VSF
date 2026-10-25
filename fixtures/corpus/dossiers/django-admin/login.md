# Django administration - login
Upstream: https://github.com/django/django
Framework: django-templates
Ground truth basis: Django administration documentation (ModelAdmin options, actions, filters, pagination, change forms); Chromium accessibility tree
Page title: Log in | Django site admin
State: as loaded
What people do here (from the manifest):
- sign in to the administration site

## Accessibility tree (Chromium, full page)
```yaml
- link "Skip to main content":
  - /url: "#content-start"
- banner:
  - link "Django administration":
    - /url: /admin/
  - 'button "Toggle theme (current theme: auto)"'
- main:
  - text: "Username:"
  - textbox "Username:"
  - text: "Password:"
  - textbox "Password:"
  - button "Log in"
- contentinfo
```
