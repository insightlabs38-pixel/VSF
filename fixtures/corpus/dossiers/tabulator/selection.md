# Tabulator examples - selection
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 07 Row Selection
State: as loaded
What people do here (from the manifest):
- select rows and act on the selection

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "07 · Row Selection" [level=1]
  - paragraph:
    - text: Tick the checkbox column to select rows (the header checkbox toggles all). The buttons below select/deselect via the API, and the panel reflects the current selection live.
    - link "← back to index":
      - /url: index.html
  - button "Select all"
  - button "Deselect all"
  - button "Log selected data"
  - grid:
    - rowgroup:
      - row "Select Row Name Department Age":
        - columnheader "Select Row":
          - checkbox "Select Row"
        - columnheader "Name"
        - columnheader "Department"
        - columnheader "Age"
    - rowgroup:
      - row "Select Row Oli Bob Engineering 28":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Oli Bob"
        - gridcell "Engineering"
        - gridcell "28"
      - row "Select Row Mary May Sales 41":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Mary May"
        - gridcell "Sales"
        - gridcell "41"
      - row "Select Row Chris L. Engineering 22":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Chris L."
        - gridcell "Engineering"
        - gridcell "22"
      - row "Select Row Brendon P. Marketing 35":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Brendon P."
        - gridcell "Marketing"
        - gridcell "35"
      - row "Select Row Margret M. Sales 26":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Margret M."
        - gridcell "Sales"
        - gridcell "26"
      - row "Select Row Van Ng Engineering 51":
        - gridcell "Select Row":
          - checkbox "Select Row"
        - gridcell "Van Ng"
        - gridcell "Engineering"
        - gridcell "51"
  - paragraph:
    - strong: "Selected:"
    - text: 0 row(s)
  - text: —
```
