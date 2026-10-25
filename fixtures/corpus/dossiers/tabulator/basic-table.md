# Tabulator examples - basic-table
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 01 Basic Table
State: as loaded
What people do here (from the manifest):
- read a table of records

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "01 · Basic Table" [level=1]
  - paragraph:
    - text: "The simplest possible Tabulator: an array of data, a few column definitions, a fit-to-width layout, and click-to-sort columns."
    - link "← back to index":
      - /url: index.html
  - grid:
    - rowgroup:
      - row "ID Name Age Country Rating":
        - columnheader "ID"
        - columnheader "Name"
        - columnheader "Age"
        - columnheader "Country"
        - columnheader "Rating"
    - rowgroup:
      - row "1 Oli Bob 12 United Kingdom 1":
        - gridcell "1"
        - gridcell "Oli Bob"
        - gridcell "12"
        - gridcell "United Kingdom"
        - gridcell "1"
      - row "2 Mary May 1 Germany 2":
        - gridcell "2"
        - gridcell "Mary May"
        - gridcell "1"
        - gridcell "Germany"
        - gridcell "2"
      - row "3 Christine Lobowski 42 France 0":
        - gridcell "3"
        - gridcell "Christine Lobowski"
        - gridcell "42"
        - gridcell "France"
        - gridcell "0"
      - row "4 Brendon Philips 125 USA 1":
        - gridcell "4"
        - gridcell "Brendon Philips"
        - gridcell "125"
        - gridcell "USA"
        - gridcell "1"
      - row "5 Margret Marmajuke 16 Sweden 5":
        - gridcell "5"
        - gridcell "Margret Marmajuke"
        - gridcell "16"
        - gridcell "Sweden"
        - gridcell "5"
      - row "6 Van Ng 37 Vietnam 4":
        - gridcell "6"
        - gridcell "Van Ng"
        - gridcell "37"
        - gridcell "Vietnam"
        - gridcell "4"
      - row "7 Duc Ng 37 Vietnam 3":
        - gridcell "7"
        - gridcell "Duc Ng"
        - gridcell "37"
        - gridcell "Vietnam"
        - gridcell "3"
```
