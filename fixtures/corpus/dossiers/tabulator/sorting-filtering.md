# Tabulator examples - sorting-filtering
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 03 Sorting & Filtering
State: as loaded
What people do here (from the manifest):
- sort columns and filter rows

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "03 · Sorting & Filtering" [level=1]
  - paragraph:
    - text: Click a header to sort; hold Shift and click another to multi-sort. Each column has a header filter (text input or dropdown). The buttons below drive filtering programmatically via the API.
    - link "← back to index":
      - /url: index.html
  - button "Only active"
  - button "Age < 30"
  - button "Dept = Engineering"
  - button "Clear filters"
  - grid:
    - rowgroup:
      - row "Name Age Department Active":
        - columnheader "Name":
          - text: Name
          - searchbox
        - columnheader "Age":
          - text: Age
          - spinbutton
        - columnheader "Department":
          - text: Department
          - searchbox
        - columnheader "Active":
          - text: Active
          - checkbox [checked=mixed]
    - rowgroup:
      - row "Oli Bob 28 Engineering":
        - gridcell "Oli Bob"
        - gridcell "28"
        - gridcell "Engineering"
        - gridcell:
          - img
      - row "Mary May 41 Sales":
        - gridcell "Mary May"
        - gridcell "41"
        - gridcell "Sales"
        - gridcell:
          - img
      - row "Chris L. 22 Engineering":
        - gridcell "Chris L."
        - gridcell "22"
        - gridcell "Engineering"
        - gridcell:
          - img
      - row "Brendon P. 35 Marketing":
        - gridcell "Brendon P."
        - gridcell "35"
        - gridcell "Marketing"
        - gridcell:
          - img
      - row "Margret M. 26 Sales":
        - gridcell "Margret M."
        - gridcell "26"
        - gridcell "Sales"
        - gridcell:
          - img
      - row "Van Ng 51 Engineering":
        - gridcell "Van Ng"
        - gridcell "51"
        - gridcell "Engineering"
        - gridcell:
          - img
      - row "Duc Ng 29 Marketing":
        - gridcell "Duc Ng"
        - gridcell "29"
        - gridcell "Marketing"
        - gridcell:
          - img
      - row "Anita Bath 33 Sales":
        - gridcell "Anita Bath"
        - gridcell "33"
        - gridcell "Sales"
        - gridcell:
          - img
```
