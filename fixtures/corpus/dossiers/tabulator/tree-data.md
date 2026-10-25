# Tabulator examples - tree-data
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 09 Tree Data
State: as loaded
What people do here (from the manifest):
- expand and collapse nested rows

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "09 · Nested Tree Data" [level=1]
  - paragraph:
    - text: Hierarchical data using
    - code: dataTree
    - text: . Each row can have a
    - code: _children
    - text: array; click the expand/collapse toggle to reveal nested rows. The size column totals across the whole tree.
    - link "← back to index":
      - /url: index.html
  - button "Expand all"
  - button "Collapse all"
  - grid:
    - rowgroup:
      - row "Name Type Size (KB)":
        - columnheader "Name"
        - columnheader "Type"
        - columnheader "Size (KB)"
    - rowgroup:
      - row "src folder 0":
        - gridcell "src"
        - gridcell "folder"
        - gridcell "0"
      - row "dist folder 0":
        - gridcell "dist"
        - gridcell "folder"
        - gridcell "0"
      - row "README.md file 12":
        - gridcell "README.md"
        - gridcell "file"
        - gridcell "12"
    - row "12":
      - gridcell
      - gridcell
      - gridcell "12"
```
