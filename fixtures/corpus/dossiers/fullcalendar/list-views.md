# FullCalendar vanilla examples - list-views
Upstream: https://github.com/fullcalendar/fullcalendar
Framework: vanilla-js
Ground truth basis: FullCalendar documentation and example descriptions; Chromium accessibility tree
Page title: 
State: as loaded
What people do here (from the manifest):
- read events as a list
- switch list range

## Accessibility tree (Chromium, full page)
```yaml
- button "Previous Week"
- button "Next Week"
- button "This Week": Today
- heading "January 8 – 14, 2023" [level=2]
- tablist "Change view":
  - tab "Day view": Day
  - tab "Week view" [selected]: Week
  - tab "Month view": Month
  - tab "Year view": Year
- list "January 8 – 14, 2023":
  - listitem "January 8, 2023":
    - link "Go to Sunday": Sunday
    - link "Go to January 8, 2023": January 8, 2023
    - list "Events":
      - listitem: All-day Long Event
  - listitem "January 9, 2023":
    - link "Go to Monday": Monday
    - link "Go to January 9, 2023": January 9, 2023
    - list "Events":
      - listitem: All-day Long Event
      - listitem: 4:00pm Repeating Event
  - listitem "January 11, 2023":
    - link "Go to Wednesday": Wednesday
    - link "Go to January 11, 2023": January 11, 2023
    - list "Events":
      - listitem: All-day Conference
  - listitem "January 12, 2023":
    - link "Go to Thursday": Thursday
    - link "Go to January 12, 2023": January 12, 2023
    - list "Events":
      - listitem: All-day Conference
      - listitem: 10:30am – 12:30pm Meeting
      - listitem: 12:00pm Lunch
      - listitem: 2:30pm Meeting
      - listitem: 5:30pm Happy Hour
      - listitem: 8:00pm Dinner
  - listitem "January 13, 2023":
    - link "Go to Friday": Friday
    - link "Go to January 13, 2023": January 13, 2023
    - list "Events":
      - listitem: 7:00am Birthday Party
```
