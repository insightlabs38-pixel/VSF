# Tabulator examples - editing
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 04 Editing
State: as loaded
What people do here (from the manifest):
- edit cells in place

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "04 · Inline Editing & Validation" [level=1]
  - paragraph:
    - text: Double-click (or click) a cell to edit it. This shows the
    - code: input
    - text: ","
    - code: number
    - text: ","
    - code: list
    - text: ","
    - code: star
    - text: ","
    - code: tickCross
    - text: and the built-in
    - code: date
    - text: editors, plus validators. Edits are logged below.
    - link "← back to index":
      - /url: index.html
  - grid:
    - rowgroup:
      - row "Name Location Progress Gender Rating Date Of Birth Driver":
        - columnheader "Name"
        - columnheader "Location"
        - columnheader "Progress"
        - columnheader "Gender"
        - columnheader "Rating"
        - columnheader "Date Of Birth"
        - columnheader "Driver"
    - rowgroup:
      - row "Oli Bob United Kingdom 12 male 1 2005-08-14":
        - gridcell "Oli Bob"
        - gridcell "United Kingdom"
        - gridcell "12"
        - gridcell "male"
        - gridcell "1":
          - img
          - img
          - img
          - img
          - img
        - gridcell "2005-08-14"
        - gridcell:
          - img
      - row "Mary May Germany 90 female 4 1989-02-01":
        - gridcell "Mary May"
        - gridcell "Germany"
        - gridcell "90"
        - gridcell "female"
        - gridcell "4":
          - img
          - img
          - img
          - img
          - img
        - gridcell "1989-02-01"
        - gridcell:
          - img
      - row "Chris L. France 42 male 0 1980-11-23":
        - gridcell "Chris L."
        - gridcell "France"
        - gridcell "42"
        - gridcell "male"
        - gridcell "0":
          - img
          - img
          - img
          - img
          - img
        - gridcell "1980-11-23"
        - gridcell:
          - img
      - row "Margret M. Sweden 16 female 5 1998-06-30":
        - gridcell "Margret M."
        - gridcell "Sweden"
        - gridcell "16"
        - gridcell "female"
        - gridcell "5":
          - img
          - img
          - img
          - img
          - img
        - gridcell "1998-06-30"
        - gridcell:
          - img
  - heading "Edit log" [level=2]
  - text: No edits yet — change a cell above.
```
