# Django administration - index
Upstream: https://github.com/django/django
Framework: django-templates
Ground truth basis: Django administration documentation (ModelAdmin options, actions, filters, pagination, change forms); Chromium accessibility tree
Page title: Site administration | Django site admin
State: signed in, site index
What people do here (from the manifest):
- move to a model's list or add page

## Accessibility tree (Chromium, full page)
```yaml
- link "Skip to main content":
  - /url: "#content-start"
- banner:
  - link "Django administration":
    - /url: /admin/
  - text: Welcome,
  - strong: admin
  - text: .
  - link "View site":
    - /url: /
  - text: /
  - link "Change password":
    - /url: /admin/password_change/
  - text: /
  - button "Log out"
  - 'button "Toggle theme (current theme: auto)"'
- main:
  - heading "Site administration" [level=1]
  - table "Authentication and Authorization":
    - caption:
      - link "Authentication and Authorization":
        - /url: /admin/auth/
    - rowgroup:
      - row "Model name Add link Change or view list link":
        - columnheader "Model name"
        - columnheader "Add link"
        - columnheader "Change or view list link"
    - rowgroup:
      - row "Groups Add Change":
        - rowheader "Groups":
          - link "Groups":
            - /url: /admin/auth/group/
        - cell "Add":
          - link "Add":
            - /url: /admin/auth/group/add/
        - cell "Change":
          - link "Change":
            - /url: /admin/auth/group/
      - row "Users Add Change":
        - rowheader "Users":
          - link "Users":
            - /url: /admin/auth/user/
        - cell "Add":
          - link "Add":
            - /url: /admin/auth/user/add/
        - cell "Change":
          - link "Change":
            - /url: /admin/auth/user/
  - table "Shop":
    - caption:
      - link "Shop":
        - /url: /admin/shop/
    - rowgroup:
      - row "Model name Add link Change or view list link":
        - columnheader "Model name"
        - columnheader "Add link"
        - columnheader "Change or view list link"
    - rowgroup:
      - row "Categories Add Change":
        - rowheader "Categories":
          - link "Categories":
            - /url: /admin/shop/category/
        - cell "Add":
          - link "Add":
            - /url: /admin/shop/category/add/
        - cell "Change":
          - link "Change":
            - /url: /admin/shop/category/
      - row "Products Add Change":
        - rowheader "Products":
          - link "Products":
            - /url: /admin/shop/product/
        - cell "Add":
          - link "Add":
            - /url: /admin/shop/product/add/
        - cell "Change":
          - link "Change":
            - /url: /admin/shop/product/
  - heading "Recent actions" [level=2]
  - heading "My actions" [level=3]
  - paragraph: None available
- contentinfo
```
