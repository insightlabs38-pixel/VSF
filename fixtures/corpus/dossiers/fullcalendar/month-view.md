# FullCalendar vanilla examples - month-view
Upstream: https://github.com/fullcalendar/fullcalendar
Framework: vanilla-js
Ground truth basis: FullCalendar documentation and example descriptions; Chromium accessibility tree
Page title: 
State: as loaded
What people do here (from the manifest):
- browse a month of events
- move between months

## Accessibility tree (Chromium, full page)
```yaml
- heading "January 2023" [level=2]
- button "This Month": Today
- button "Previous Month"
- button "Next Month"
- table "January 2023":
  - rowgroup:
    - row "Sunday Monday Tuesday Wednesday Thursday Friday Saturday":
      - columnheader "Sunday"
      - columnheader "Monday"
      - columnheader "Tuesday"
      - columnheader "Wednesday"
      - columnheader "Thursday"
      - columnheader "Friday"
      - columnheader "Saturday"
  - rowgroup:
    - row "Week 1":
      - cell "January 1, 2023 All Day Event":
        - text: January 1, 2023
        - list:
          - listitem: All Day Event
      - cell "January 2, 2023"
      - cell "January 3, 2023"
      - cell "January 4, 2023"
      - cell "January 5, 2023"
      - cell "January 6, 2023"
      - cell "January 7, 2023 Long Event":
        - text: January 7, 2023
        - list:
          - listitem: Long Event
    - row "Week 2":
      - cell "January 8, 2023 Long Event":
        - text: January 8, 2023
        - list:
          - listitem: Long Event
      - cell "January 9, 2023 4p Repeating Event":
        - text: January 9, 2023
        - list:
          - listitem: 4p Repeating Event
      - cell "January 10, 2023"
      - cell "January 11, 2023 Conference":
        - text: January 11, 2023
        - list:
          - listitem: Conference
      - cell "January 12, 2023 10:30a Meeting 12p Lunch +3 more":
        - text: January 12, 2023
        - list:
          - listitem: 10:30a Meeting
          - listitem: 12p Lunch
        - button "+3 more"
      - cell "January 13, 2023 7a Birthday Party":
        - text: January 13, 2023
        - list:
          - listitem: 7a Birthday Party
      - cell "January 14, 2023"
    - row "Week 3":
      - cell "January 15, 2023"
      - cell "January 16, 2023 4p Repeating Event":
        - text: January 16, 2023
        - list:
          - listitem: 4p Repeating Event
      - cell "January 17, 2023"
      - cell "January 18, 2023"
      - cell "January 19, 2023"
      - cell "January 20, 2023"
      - cell "January 21, 2023"
    - row "Week 4":
      - cell "January 22, 2023"
      - cell "January 23, 2023"
      - cell "January 24, 2023"
      - cell "January 25, 2023"
      - cell "January 26, 2023"
      - cell "January 27, 2023"
      - cell "January 28, 2023 Click for Google":
        - text: January 28, 2023
        - list:
          - listitem:
            - link "Click for Google":
              - /url: http://google.com/
    - row "Week 5":
      - cell "January 29, 2023"
      - cell "January 30, 2023"
      - cell "January 31, 2023"
      - cell "February 1, 2023"
      - cell "February 2, 2023"
      - cell "February 3, 2023"
      - cell "February 4, 2023"
    - row "Week 6":
      - cell "February 5, 2023"
      - cell "February 6, 2023"
      - cell "February 7, 2023"
      - cell "February 8, 2023"
      - cell "February 9, 2023"
      - cell "February 10, 2023"
      - cell "February 11, 2023"
```
