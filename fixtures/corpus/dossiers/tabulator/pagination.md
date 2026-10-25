# Tabulator examples - pagination
Upstream: https://github.com/olifolkerd/tabulator
Framework: vanilla-js
Ground truth basis: Tabulator documentation and example descriptions; Chromium accessibility tree
Page title: Tabulator Example — 05 Pagination
State: as loaded
What people do here (from the manifest):
- move through pages of records
- change the page size

## Accessibility tree (Chromium, full page)
```yaml
- main:
  - heading "05 · Pagination" [level=1]
  - paragraph:
    - text: Client-side pagination over a larger generated dataset. Use the page controls at the bottom, change the page size with the selector, and watch the row counter.
    - link "← back to index":
      - /url: index.html
  - grid:
    - rowgroup:
      - row "ID Name Department Age Score":
        - columnheader "ID"
        - columnheader "Name"
        - columnheader "Department"
        - columnheader "Age"
        - columnheader "Score"
    - rowgroup:
      - 'row "1 Mary #1 Sales 25 37"':
        - gridcell "1"
        - 'gridcell "Mary #1"'
        - gridcell "Sales"
        - gridcell "25"
        - gridcell "37"
      - 'row "2 Chris #2 Marketing 32 74"':
        - gridcell "2"
        - 'gridcell "Chris #2"'
        - gridcell "Marketing"
        - gridcell "32"
        - gridcell "74"
      - 'row "3 Brendon #3 Support 39 11"':
        - gridcell "3"
        - 'gridcell "Brendon #3"'
        - gridcell "Support"
        - gridcell "39"
        - gridcell "11"
      - 'row "4 Margret #4 Finance 46 48"':
        - gridcell "4"
        - 'gridcell "Margret #4"'
        - gridcell "Finance"
        - gridcell "46"
        - gridcell "48"
      - 'row "5 Van #5 Engineering 53 85"':
        - gridcell "5"
        - 'gridcell "Van #5"'
        - gridcell "Engineering"
        - gridcell "53"
        - gridcell "85"
      - 'row "6 Duc #6 Sales 60 22"':
        - gridcell "6"
        - 'gridcell "Duc #6"'
        - gridcell "Sales"
        - gridcell "60"
        - gridcell "22"
      - 'row "7 Anita #7 Marketing 22 59"':
        - gridcell "7"
        - 'gridcell "Anita #7"'
        - gridcell "Marketing"
        - gridcell "22"
        - gridcell "59"
      - 'row "8 Sam #8 Support 29 96"':
        - gridcell "8"
        - 'gridcell "Sam #8"'
        - gridcell "Support"
        - gridcell "29"
        - gridcell "96"
      - 'row "9 Priya #9 Finance 36 33"':
        - gridcell "9"
        - 'gridcell "Priya #9"'
        - gridcell "Finance"
        - gridcell "36"
        - gridcell "33"
      - 'row "10 Oli #10 Engineering 43 70"':
        - gridcell "10"
        - 'gridcell "Oli #10"'
        - gridcell "Engineering"
        - gridcell "43"
        - gridcell "70"
    - text: Showing 1-10 of 200 rows Page Size
    - combobox "Page Size":
      - option "10" [selected]
      - option "25"
      - option "50"
      - option "100"
    - button "First Page" [disabled]: First
    - button "Prev Page" [disabled]: Prev
    - button "Show Page 1": "1"
    - button "Show Page 2": "2"
    - button "Show Page 3": "3"
    - button "Show Page 4": "4"
    - button "Show Page 5": "5"
    - button "Next Page": Next
    - button "Last Page": Last
```
