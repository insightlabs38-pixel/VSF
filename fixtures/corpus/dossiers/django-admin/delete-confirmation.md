# Django administration - delete-confirmation
Upstream: https://github.com/django/django
Framework: django-templates
Ground truth basis: Django administration documentation (ModelAdmin options, actions, filters, pagination, change forms); Chromium accessibility tree
Page title: Delete | Django site admin
State: signed in, delete confirmation
What people do here (from the manifest):
- confirm or cancel deleting a product

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
- navigation "Breadcrumbs":
  - link "Home":
    - /url: /admin/
  - text: ›
  - link "Shop":
    - /url: /admin/shop/
  - text: ›
  - link "Products":
    - /url: /admin/shop/product/
  - text: ›
  - link "Product 01":
    - /url: /admin/shop/product/1/change/
  - text: › Delete
- button "Toggle navigation": «
- navigation "Sidebar":
  - searchbox "Filter navigation items"
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
      - row "Groups Add":
        - rowheader "Groups":
          - link "Groups":
            - /url: /admin/auth/group/
        - cell "Add":
          - link "Add":
            - /url: /admin/auth/group/add/
      - row "Users Add":
        - rowheader "Users":
          - link "Users":
            - /url: /admin/auth/user/
        - cell "Add":
          - link "Add":
            - /url: /admin/auth/user/add/
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
      - row "Categories Add":
        - rowheader "Categories":
          - link "Categories":
            - /url: /admin/shop/category/
        - cell "Add":
          - link "Add":
            - /url: /admin/shop/category/add/
      - row "Products Add":
        - rowheader "Products":
          - link "Products":
            - /url: /admin/shop/product/
        - cell "Add":
          - link "Add":
            - /url: /admin/shop/product/add/
- main:
  - heading "Delete" [level=1]
  - paragraph: "Are you sure you want to delete the product \"Product 01\"? All of the following related items will be deleted:"
  - heading "Summary" [level=2]
  - list:
    - listitem: "Products: 1"
  - heading "Objects" [level=2]
  - list:
    - listitem:
      - text: "Product:"
      - link "Product 01":
        - /url: /admin/shop/product/1/change/
  - button "Yes, I’m sure"
  - link "No, take me back":
    - /url: "#"
- contentinfo
```
